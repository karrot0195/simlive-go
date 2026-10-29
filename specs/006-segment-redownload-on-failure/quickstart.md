# Quickstart: Validate Segment Re-download on Failure

## Prerequisites

- Running planner + engine + Redis + Postgres stack.
- A channel with timeline segments available.

## Validation Steps

1. Start services: `make start-all`.
2. Force a failed download (e.g., temporarily break rclone access or remove a remote segment) and observe engine logs for preparation failure with channel/sequence.
3. Confirm the segment is NOT present in the rendition playlist (`playlist.m3u8`) while failed.
4. Restore access; confirm the segment is re-downloaded and appears in the playlist within two playout cycles.
5. Create a zero-byte `seg_<seq>.ts` manually; confirm it is deleted and re-downloaded, not served.