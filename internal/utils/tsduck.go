package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"simlive/internal/configs"
	"simlive/internal/models"
	"strings"
	"time"
)

const TSP_BINARY = "tsp"  // Ensure tsp is in PATH
const SCTE35_PID = "0x30" // Typical PID for SCTE-35

const PTS_MASK int64 = 0x1FFFFFFFF

// generateSpliceInsertXml generates TSDuck XML for a splice_insert command.
// Conforms to TSDuck's spliceinject plugin expected XML format.
func generateSpliceInsertXml(event models.SpliceInsert) string {
	// Normalize cuein/cueout to in/out
	normalizedType := normalizeType(event.Type)
	// Determine out_of_network
	outOfNetwork := normalizedType == "out"
	hasValidDuration := normalizedType == "out" && event.Duration > 0
	// Duration info is only meaningful on the slice point (0x34)
	includeDuration := hasValidDuration

	// SCTE-35 segmentation_descriptor
	segmentationDescriptor := generateSegmentationDescriptor(event, includeDuration)

	// Use splice_immediate="false" so that pts_time is honored.
	// pts_time is a 33-bit field; mask epoch-based PTS down to fit.
	var ptsTime string
	breakDuration := ""
	if includeDuration {
		breakDuration = fmt.Sprintf("\n      <break_duration auto_return=\"true\" duration=\"%d\"/>", int64(event.Duration*90000))
	}

	sliceImmediate := "false"

	if event.SegmentationTypeID == configs.SEG_TYPE_ADVERTISEMENT_START || event.SegmentationTypeID == configs.SEG_TYPE_ADVERTISEMENT_END {
		sliceImmediate = "true"
	} else {
		ptsTime = fmt.Sprintf(`pts_time="%d"`, event.SpliceTimePTS&PTS_MASK)
	}

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<tsduck>
  <splice_information_table>
    <splice_insert
        splice_event_id="%d"
        unique_program_id="%d"
        out_of_network="%t"
        %s
        splice_immediate="%s">%s
    </splice_insert>
	%s
  </splice_information_table>
</tsduck>`, event.EventID, event.UniqueProgramID, outOfNetwork, ptsTime, sliceImmediate, breakDuration, segmentationDescriptor)

	return xml
}

func normalizeType(t string) string {
	switch t {
	case "cueout":
		return "out"
	case "cuein":
		return "in"
	}
	return t
}

func generateSegmentationDescriptor(event models.SpliceInsert, includeDuration bool) string {
	subSegment := ""
	if event.SegmentationTypeID == configs.SEG_TYPE_ADVERTISEMENT_START || event.SegmentationTypeID == configs.SEG_TYPE_PLACE_OPPORTUNITY_END {
		subSegment = "\n        sub_segment_num=\"0\"\n        sub_segments_expected=\"0\""
	}

	duration := ""
	if includeDuration {
		duration = fmt.Sprintf("\n        segmentation_duration=\"%d\"", int64(event.Duration*90000))
	}

	return fmt.Sprintf(`      <splice_segmentation_descriptor
        segmentation_event_id="%d"
        segmentation_type_id="0x%02x"
        segment_num="0"
        segments_expected="0"%s%s>
      <segmentation_upid type="0x00"/>
    </splice_segmentation_descriptor>`, event.EventID, event.SegmentationTypeID, subSegment, duration)
}

// InjectSpliceEvent runs TSDuck tsp to inject SCTE-35 XML into a TS file.
// @param inputFile Path to source TS file
// @param outputFile Path to output TS file
// @param xmlContent XML string to inject
// @returns true on success
func InjectSpliceEvent(parentCtx context.Context, inputFile string, outputFile string, se models.SpliceInsert) error {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	xmlContent := generateSpliceInsertXml(se)

	xmlFile, err := os.CreateTemp("", "scte35_*.xml")
	defer func() {
		xmlFile.Close()
		os.Remove(xmlFile.Name())
	}()
	if err != nil {
		return Error("[SCTE35] event=tsduck_xml_create_failed error=%v", err)
	}

	if _, err := xmlFile.WriteString(xmlContent); err != nil {
		return Error("[SCTE35] event=tsduck_xml_write_failed error=%v", err)
	}

	// Cleaned up pipeline:
	// 1. PMT plugin adds stream type 0x86 (SCTE-35) and registration descriptor "CUEI" (0x43554549).
	// 2. Spliceinject plugin reads XML and injects SCTE-35 sections into the specified PID.
	//    --service auto-resolves the video PID (used as the PTS reference) from the PMT.
	command := fmt.Sprintf(
		`%s --add-input-stuffing 1/10 -I file "%s" -P pmt --add-pid %s/0x86 --add-registration 0x43554549 -P spliceinject --service 1 --pid %s --files "%s" --wait-first-batch -O file "%s"`,
		TSP_BINARY, inputFile, SCTE35_PID, SCTE35_PID, xmlFile.Name(), outputFile,
	)

	var out bytes.Buffer

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return Error("[SCTE35] event=tsduck_injection_failed file=%s error=%v", inputFile, err)
	}

	if strings.Contains(out.String(), "Error") {
		return Error("[SCTE35] event=tsduck_injection_failed file=%s error=%v", inputFile, out)

	}

	return nil
}

func generateXmlForAds(seg models.SegmentItem) *string {
	adsPtsOffset := int32((seg.PtsOffset)*configs.TICKS) + 1*configs.TICKS
	adsDuration := int32(seg.AdBreakDuration * configs.TICKS)
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<tsduck>
  <splice_information_table>
    <splice_insert splice_event_id="%d" unique_program_id="%d"
                   out_of_network="true" pts_time="%d"
                   splice_immediate="false">
      <break_duration auto_return="true" duration="%d"/>
    </splice_insert>
  </splice_information_table>
</tsduck>`, seg.AdBreakID, seg.AdBreakID, adsPtsOffset, adsDuration)

	xmlPath := fmt.Sprintf("%s/ads-%f.xml", os.TempDir(), seg.RunningTime)
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		return nil
	}
	return &xmlPath
}

// ConfigurePmtForAdsTs runs TSDuck tsp to register the SCTE-35 PID in the PMT of a TS file,
// without injecting any splice command. Used for ad segments that carry no
// splice event but must still declare the SCTE-35 PID in their PMT.
func ConfigurePmtForAdsTs(parentCtx context.Context, seg models.SegmentItem, inputFile string, outputFile string) error {

	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()

	command := fmt.Sprintf(
		"%s -a 1/10 -I file \"%s\" -P pmt --add-pid 0x1F00/0x86 --add-registration 0x43554549 -O file \"%s\"",
		TSP_BINARY, inputFile, outputFile,
	)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return Error("[SCTE35] event=pmt_config_failed file=%s error=%v", inputFile, err)
	}
	return nil
}

func IsDev() bool {
	return GetEnv("IS_DEV", "false") == "true"
}
