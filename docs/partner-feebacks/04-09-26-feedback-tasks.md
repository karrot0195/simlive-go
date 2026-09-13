
- [x] One SCTE-35 issue remains: the declaration is not continuous - I think I know the reason. And I will fix it.

- [x] Pre-roll is shorter than described: verified. we always have pre-roll 8s and 4s

- CUE IN appeared too early: correct. because we just have CUE IN at immediately -> support CUE IN  pre-roll -> 4s
(4) -> Implemented the logic -> will verify after lunch 
(3) Implement the logic 


    * Event placement (Preroll, CUE IN/OUT): Correct
    * Event type: Correct
    * Splicemonitoring: Not yet accurate as expected 

- `The splice time now references the delivered media clock. One example we captured carries splice_pts 1251535330, about 13905.9 seconds, against a segment PCR of 13904.9 seconds. Previously these values were roughly 75 minutes away from the media clock, so this is resolved.
The publish rate has also improved. The stream now produces a new segment every 3.91 seconds against a declared 4 seconds, which is fine. An earlier version of this stream was running about 12 percent slow, which drained our buffer and forced a reset roughly every 70 seconds.`
   + Implement logic: DONE 
   + Testing: TODO 