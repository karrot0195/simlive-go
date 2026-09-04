package utils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"simlive/internal/models"
)

const TSP_BINARY = "tsp"  // Ensure tsp is in PATH
const SCTE35_PID = "0x30" // Typical PID for SCTE-35

const logger = "TsDuckUtils"

const SEG_TYPE_ADVERTISEMENT_START = 0x34 // Advertisement Start (slice point)

// PTS_MASK is the 33-bit MPEG-TS PTS range (2^33 - 1). SCTE-35 pts_time is a
// 33-bit field, so epoch-based PTS values (which exceed 2^33/90000 ≈ 95443.7s)
// must be masked down to fit. TSDuck rejects pts_time outside [0, 8589934591].
const PTS_MASK int64 = 0x1FFFFFFFF

// SpliceEvent represents a splice event for TSDuck XML generation.
// This is a Go equivalent of the TypeScript SpliceEvent type.

// GenerateSpliceInsertXml generates TSDuck XML for a splice_insert command.
// Conforms to TSDuck's inject plugin expected XML format.
func GenerateSpliceInsertXml(event models.SpliceInsert) string {
	// Normalize cuein/cueout to in/out
	normalizedType := normalizeType(event.Type)
	// Determine out_of_network
	outOfNetwork := normalizedType == "out"
	hasValidDuration := normalizedType == "out" && event.Duration > 0
	// Duration info is only meaningful on the slice point (0x34)
	includeDuration := hasValidDuration && (event.SegmentationTypeID == 0 || event.SegmentationTypeID == SEG_TYPE_ADVERTISEMENT_START)

	// SCTE-35 segmentation_descriptor
	segmentationDescriptor := generateSegmentationDescriptor(event, includeDuration)

	// Use splice_immediate="false" so that pts_time is honored.
	// pts_time is a 33-bit field; mask epoch-based PTS down to fit.
	ptsTime := event.SpliceTimePTS & PTS_MASK
	breakDuration := ""
	if includeDuration {
		breakDuration = fmt.Sprintf("\n      <break_duration auto_return=\"true\" duration=\"%d\"/>", int64(event.Duration*90000))
	}

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<tsduck>
  <splice_information_table>
    <splice_insert
        splice_event_id="%d"
        unique_program_id="%d"
        out_of_network="%t"
        pts_time="%d"
        splice_immediate="false">%s
    </splice_insert>%s
  </splice_information_table>
</tsduck>`, event.EventID, event.UniqueProgramID, outOfNetwork, ptsTime, breakDuration, segmentationDescriptor)

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
	if event.SegmentationTypeID == SEG_TYPE_ADVERTISEMENT_START {
		subSegment = "\n        sub_segment_num=\"0\"\n        sub_segments_expected=\"0\""
	}

	duration := ""
	if includeDuration {
		duration = fmt.Sprintf("\n        segmentation_duration=\"%d\"", int64(event.Duration*90000))
	}

	return fmt.Sprintf(`
    <splice_segmentation_descriptor
        segmentation_event_id="%d"
        segmentation_type_id="0x%x"
        segment_num="0"
        segments_expected="0"%s%s>
      <segmentation_upid type="0x00"/>
    </splice_segmentation_descriptor>`, event.EventID, event.SegmentationTypeID, subSegment, duration)
}

// InjectSpliceEvent runs TSDuck tsp to inject SCTE-35 XML into a TS file.
// @param inputFile   Path to source TS file
// @param outputFile  Path to output TS file (can be same as inputFile, but we'll use temp file)
// @param xmlContent  XML string to inject
// @returns           true on success
func InjectSpliceEvent(inputFile string, outputFile string, xmlContent string) bool {
	// Tạo file XML tạm thời để chứa nội dung
	xmlFile, err := os.CreateTemp("", "scte35_*.xml")
	if err != nil {
		log.Printf("Failed to create temp XML file: %v", err)
		return false
	}
	defer os.Remove(xmlFile.Name())

	if _, err := xmlFile.WriteString(xmlContent); err != nil {
		log.Printf("Failed to write XML content: %v", err)
		return false
	}
	xmlFile.Close()

	// Sử dụng spliceinject thay vì inject
	command := fmt.Sprintf(
		"%s -a 1/10 -I file \"%s\" -P pmt --add-pid %s/0x86 --add-registration 0x43554549 -P spliceinject --service 1 --files \"%s\" --wait-first-batch -P filter --negate --pid 0x1FFF -O file \"%s\"",
		TSP_BINARY, inputFile, SCTE35_PID, xmlFile.Name(), outputFile,
	)

	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Printf("TSDuck injection failed for %s: %v", inputFile, err)
		return false
	}

	// Verify với spliceinject thay vì splicemonitor (hoặc giữ nguyên)
	if isDev() {
		verifyCmd := fmt.Sprintf(
			"%s -I file \"%s\" -P splicemonitor --all-commands -O drop | head -n 15",
			TSP_BINARY, outputFile,
		)
		cmd := exec.Command("sh", "-c", verifyCmd)
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Printf("SCTE-35 splice verification failed for %s: %v", outputFile, err)
		}
	}

	log.Printf("TSDuck injection succeeded for %s", inputFile)
	return true
}

// ConfigurePmtForScte35 runs TSDuck tsp to register the SCTE-35 PID in the PMT of a TS file,
// without injecting any splice command. Used for ad segments that carry no
// splice event but must still declare the SCTE-35 PID in their PMT.
func ConfigurePmtForScte35(inputFile string, outputFile string) bool {
	// Không cần --wait-first-batch ở đây
	command := fmt.Sprintf(
		"%s -a 1/10 -I file \"%s\" -P pmt --add-pid %s/0x86 --add-registration 0x43554549 -O file \"%s\"",
		TSP_BINARY, inputFile, SCTE35_PID, outputFile,
	)
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("PMT configuration failed for %s: %v", inputFile, err)
		return false
	}
	log.Printf("PMT configuration succeeded for %s", inputFile)
	return true
}

func isDev() bool {
	return false // TODO: Implement isDev logic
}
