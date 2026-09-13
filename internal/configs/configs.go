package configs

const TZ = "Asia/Saigon"
const TZ_OFFSET = 7 * 3600
const TZ_NUM = "+07:00"

const DATE_FORMAT_YYYY_MM_DD_HHMMSS = "2006-01-02 15:04:05"
const DATE_FORMAT_YYYY_MM_DD = "2006-01-02"

const SEG_TYPE_ADVERTISEMENT_START = 0x34     // Advertisement Start (slice point)
const SEG_TYPE_ADVERTISEMENT_END = 0x35       // Advertisement Start (slice point)
const SEG_TYPE_PLACE_OPPORTUNITY_START = 0x30 // Pre-roll (8s/4s warning)
const SEG_TYPE_PLACE_OPPORTUNITY_END = 0x36   // Post-roll (8s/4s warning)

const PTS_DURATION = (1001.0 / 24000.0) * TICKS
const TICKS = 90000.0
