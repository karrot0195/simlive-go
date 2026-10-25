# Interface Contracts: Static SCTE-35 PMT

**Date**: 2026-10-23 | **Feature**: [Static SCTE-35 PMT](../spec.md)

## Contract 1: `ConfigurePmtForVodTs` (new function)

**File**: `internal/utils/tsduck.go`

**Signature**:
```go
func ConfigurePmtForVodTs(parentCtx context.Context, seg models.SegmentItem, inputFile string, outputFile string) error
```

**Purpose**: Configure the PMT of a VOD segment to declare the SCTE-35 elementary stream (stream_type 0x86) with PID `0x1F00` and registration descriptor `0x43554549`, without injecting any splice command.

**Preconditions**:
- `inputFile` exists and is a valid MPEG-TS file
- `tsp` binary is available in PATH
- `parentCtx` is not cancelled

**Postconditions**:
- On success: `outputFile` contains the same content as `inputFile` plus the SCTE-35 stream declared in the PMT
- On failure: `outputFile` may not exist; caller must clean up

**TSDuck command**:
```sh
tsp -a 1/10 -I file "<inputFile>" \
    -P pmt --add-pid 0x1F00/0x86 --add-registration 0x43554549 \
    -O file "<outputFile>"
```

**Error handling**: Returns error if `tsp` command fails or times out (5s timeout). Caller must retry once per FR-006.

---

## Contract 2: `applySliceEvents` (modified)

**File**: `internal/engine/engine.go`

**Signature**:
```go
func (e *Engine) applySliceEvents(parentCtx context.Context, seg models.SegmentItem, dest, channelID string) error
```

**Modified behavior**: In addition to existing splice injection and ad PMT configuration, now also configures PMT for VOD segments without splice events.

**Decision logic** (pseudocode):
```
if segment has splice events:
    for each event: InjectSpliceEvent()  // existing — adds SCTE-35 to PMT + splice command
else if segment is ad:
    ConfigurePmtForAdsTs()  // existing — adds SCTE-35 to PMT
else:
    ConfigurePmtForVodTs()  // NEW — adds SCTE-35 to PMT for plain VOD segments
```

**Error handling**: On `ConfigurePmtForVodTs`, `ConfigurePmtForAdsTs`, or `InjectSpliceEvent` failure, retry once. If still fails, log the error (with channelID, segment sequence, and dest) and return error (segment will be skipped upstream).

---

## Contract 3: `tsp` binary (external)

**Binary**: `tsp` (TSDuck)

**Version**: As installed in the Docker image / deployment environment.

**Relevant command**:
```sh
tsp -a <adaptation-stuffing> -I file "<input>" \
    -P pmt --add-pid <pid>/<stream_type> [--add-registration <reg>] \
    -O file "<output>"
```

**Flags**:
- `-a <ratio>`: Add adaptation field stuffing (1/10 = 1 byte per 10 packets)
- `-I file`: File input plugin
- `-P pmt`: PMT plugin — modifies the Program Map Table
- `--add-pid <pid>/<stream_type>`: Add an elementary stream with given PID and stream_type
- `--add-registration <hex>`: Add a registration descriptor (e.g., `0x43554549` = "CUEI")
- `-O file`: File output plugin

**Idempotency**: Adding a PID that already exists in the PMT is a no-op (produces identical output).