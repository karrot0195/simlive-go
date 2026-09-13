--

One SCTE-35 issue remains: the declaration is not continuous.

We monitored 211 consecutive segments, about 14 minutes. The SCTE-35 stream is declared in the PMT for 79 of them and absent from the other 132. The gaps are very regular: the declaration disappears for exactly 43 segments, 172 seconds, then returns, on a repeating cycle of about 4.5 minutes. It appears the PID is only declared around each ad break.

This matters because a receiver decides which streams to process when it first reads the PMT. Anything that starts during one of those 172 second windows sees only video and audio, never opens the SCTE-35 PID, and silently misses every subsequent cue. The PID needs to be declared in the PMT continuously, including between breaks.


---

Pre-roll is shorter than described. We captured a full ad break and measured the lead time between each splice message and the splice point it announces:

The break start (CUE-OUT) gives at most 4.0 seconds of lead time, not the 8 seconds mentioned earlier.
The return (CUE-IN) gives only 1.0 second, which is too short to act on.
The same event is announced several times with a different splice time in each segment, and the last announcements carry a splice time that has already passed, up to 3.3 seconds in the past. Repeated messages for one event should carry the same splice time.
Your two questions

fMP4 (.m4s) segments: no, our ingest is MPEG-TS only. It reads 188 byte transport stream packets, uses the PAT and PMT to find the elementary streams, and re-emits MPEG-TS. Fragmented MP4 and CMAF are not supported, so please keep the playout on .ts segments. Changing format would not help us and would cost you a re-transcode.

Multiple audio languages and subtitle tracks: these need to be inside the transport stream as additional elementary streams declared in the PMT. We do not fetch separate audio or subtitle renditions referenced by EXT-X-MEDIA in the master playlist, so tracks delivered that way would not reach us. There is no requirement for additional languages or subtitles from our side today, but if they are wanted later, in-band in the TS is the way to deliver them.

Once the PMT declaration is continuous, send the URL and we will verify within one working day so you are not held up again.