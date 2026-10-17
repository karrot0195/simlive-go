### ESPX Server XML Retrieval Guidelines
#### Parameter Support
| Parameter | Format / Value | Default | Notes |
| --- | --- | --- | --- |
| **`tz`** | `+[HH:mm]` or `-[HH:mm]` | `+00:00` | Timezone offset |
| **`startDate`** | `YYYY-MM-DD` | Today's date | Start date of the EPG XML |
| **`endDate`** | `YYYY-MM-DD` | next day from startDate  | Must **not exceed 7 days** from `startDate` |
| **`download`** | `true` or `false` | `false` | Prompts direct XML file download if `true` |

> ℹ️ **Note:** All parameters are optional and can be omitted.

---

#### API URL Structure

```http
https://api2.espx.cloud/common/epg-xml/{CHANNEL_ID}?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11

```
---

#### Channel List & XML URLs

| Channel Name | Channel ID | XML URL | Playout URL |
| --- | --- | --- | --- |
| **Old West TV (New Playout)** | `56e09314-4e76-4004-be8a-3001ead24d25` | [https://api2.espx.cloud/common/epg-xml/56e09314-4e76-4004-be8a-3001ead24d25?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://www.google.com/search?q=https://api2.espx.cloud/common/epg-xml/56e09314-4e76-4004-be8a-3001ead24d25%3Ftz%3D%2B07:00%26startDate%3D2026-10-10%26endDate%3D2026-10-11) | [https://simlive4.espx.cloud/live/56e09314-4e76-4004-be8a-3001ead24d25/playlist.m3u8](https://www.google.com/search?q=https://simlive4.espx.cloud/live/56e09314-4e76-4004-be8a-3001ead24d25/playlist.m3u8) |
| **Camera Smile TV (New Playout)** | `cdda7af8-8f9d-4882-a3c4-0b69dd7fce01` | [https://api2.espx.cloud/common/epg-xml/cdda7af8-8f9d-4882-a3c4-0b69dd7fce01?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/cdda7af8-8f9d-4882-a3c4-0b69dd7fce01?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/cdda7af8-8f9d-4882-a3c4-0b69dd7fce01/playlist.m3u8](https://simlive4.espx.cloud/live/cdda7af8-8f9d-4882-a3c4-0b69dd7fce01/playlist.m3u8) |
| **Fight TV Plus New Playout** | `e4e5176d-ee37-44a5-be69-fbeed624855d` | [https://api2.espx.cloud/common/epg-xml/e4e5176d-ee37-44a5-be69-fbeed624855d?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/e4e5176d-ee37-44a5-be69-fbeed624855d?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/e4e5176d-ee37-44a5-be69-fbeed624855d/playlist.m3u8](https://simlive4.espx.cloud/live/e4e5176d-ee37-44a5-be69-fbeed624855d/playlist.m3u8) |
| **Cartoon Classics New playout** | `0739c0f0-eee6-44f4-aa87-dfc0336e826b` | [https://api2.espx.cloud/common/epg-xml/0739c0f0-eee6-44f4-aa87-dfc0336e826b?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/0739c0f0-eee6-44f4-aa87-dfc0336e826b?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/0739c0f0-eee6-44f4-aa87-dfc0336e826b/playlist.m3u8](https://simlive4.espx.cloud/live/0739c0f0-eee6-44f4-aa87-dfc0336e826b/playlist.m3u8) |
| **Travel & Food TV New Playout** | `7512015f-00de-417c-93a4-e3f0ca2407c9` | [https://api2.espx.cloud/common/epg-xml/7512015f-00de-417c-93a4-e3f0ca2407c9?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/7512015f-00de-417c-93a4-e3f0ca2407c9?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/7512015f-00de-417c-93a4-e3f0ca2407c9/playlist.m3u8](https://simlive4.espx.cloud/live/7512015f-00de-417c-93a4-e3f0ca2407c9/playlist.m3u8) |
| **RaceCentral TV (Playout)** | `84dd40fe-689e-4b68-9190-0e67c49d138a` | [https://api2.espx.cloud/common/epg-xml/84dd40fe-689e-4b68-9190-0e67c49d138a?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/84dd40fe-689e-4b68-9190-0e67c49d138a?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/84dd40fe-689e-4b68-9190-0e67c49d138a/playlist.m3u8](https://simlive4.espx.cloud/live/84dd40fe-689e-4b68-9190-0e67c49d138a/playlist.m3u8) |
| **SportsTVPlus (Playout)** | `3db065d1-269e-45d6-95fc-0ad3f0adcf67` | [https://api2.espx.cloud/common/epg-xml/3db065d1-269e-45d6-95fc-0ad3f0adcf67?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/3db065d1-269e-45d6-95fc-0ad3f0adcf67?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/3db065d1-269e-45d6-95fc-0ad3f0adcf67/playlist.m3u8](https://simlive4.espx.cloud/live/3db065d1-269e-45d6-95fc-0ad3f0adcf67/playlist.m3u8) |
| **CelebrityScene TV (Playout)** | `f1533ebe-dc74-4e85-a34e-a9bc4eedee78` | [https://api2.espx.cloud/common/epg-xml/f1533ebe-dc74-4e85-a34e-a9bc4eedee78?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11](https://api2.espx.cloud/common/epg-xml/f1533ebe-dc74-4e85-a34e-a9bc4eedee78?tz=+07:00&startDate=2026-10-10&endDate=2026-10-11) | [https://simlive4.espx.cloud/live/f1533ebe-dc74-4e85-a34e-a9bc4eedee78/playlist.m3u8](https://simlive4.espx.cloud/live/f1533ebe-dc74-4e85-a34e-a9bc4eedee78/playlist.m3u8) |
