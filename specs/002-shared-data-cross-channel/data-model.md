# Data Model

## SegmentProcessor (per-call)
New fields:
- isBlockingProcessing bool
- processingSegments []models.SegmentItem
- adsId uint32

## Planner (shared)
Removed fields:
- isBlockingProcessing
- processingSegments
- adsId