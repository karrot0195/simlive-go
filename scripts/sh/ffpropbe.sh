FILE_PATH=espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x-v2/$1
rclone cat $FILE_PATH | \
ffprobe -v error -f mpegts -select_streams v:0 \
  -show_entries packet=pts \
  -of csv=p=0 pipe:0 | \
awk 'NR==1{start=$1} {end=$1} END{print "PTS-RANGE:"start",-"end","}'