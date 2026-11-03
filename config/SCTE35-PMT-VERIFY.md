# SCTE-35 PMT Verification Report

- Target: https://simlive5.partner.cloud/live/f1533ebe-dc74-4e85-a34e-a9bc4eedee78/360p/playlist.m3u8
- Date: 2026-10-25
- Tool: cmd/scte35-check
- Result: PASS
- Checks:
  - SCTE-35 stream present: PID 0x1F00, stream_type 0x86
  - Registration descriptor "CUEI" present
  - PMT matches baseline across consecutive segments
- Raw log: /tmp/scte2.log
