
current file: seg_1607.ts

XML file
<?xml version="1.0" encoding="UTF-8"?>\n<tsduck>\n  <splice_information_table>\n    <splice_insert\n        splice_event_id="117"\n        unique_program_id="117"\n        out_of_network="true"\n        \n        splice_immediate="true">\n    </splice_insert>\n\t      <splice_segmentation_descriptor\n        segmentation_event_id="117"\n        segmentation_type_id="0x30"\n        segment_num="0"\n        segments_expected="0">\n      <segmentation_upid type="0x00"/>\n    </splice_segmentation_descriptor>\n  </splice_information_table>\n</tsduck>"

command
tsp -I file "seg_1607.ts" -P pmt --add-pid 0x30/0x86 --add-registration 0x43554549 -P inject --inter-packet 1000 --pid 0x30 --xml "xml.xml" -O file "seg_1607.ts.splice0.tmp"

output
tsp -I file seg_1607.ts.splice0.tmp  -P splicemonitor -O drop <- empty