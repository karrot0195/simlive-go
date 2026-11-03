## Root Cause
Partner said: PMT includes inconsistent language descriptor -> resets ad state

## TODO tasks:
- [x] verify that PMT includes language descriptor -> confirmed PMT includes language descriptor
- [x] verify Celebrity Channel includes some TS with language descriptor != english -> found and confirmed exists
- [x] find solution to modify / reset language descriptor --> TSDuck supports `--audio-language`. Quick test: it can modify. But need to verify whether it impacts other information or not. Will verify during implementation
- [ ] Implement in simlive go -> DOING
- [ ] Verify all features locally. Expect no impact on current features
- [ ] Deploy new version to simlive5 + verify
