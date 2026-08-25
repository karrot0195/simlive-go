triển khai 1 project golang cho simlive



### Infrastructure:

framework: gin gonic

database: postgresql

tools: ffmpeg, tsduck, ffprobe

DB: docker-compose.yml



### Muc tiêu:

các playout channel đã đăng kí sẽ cần chạy 24 / 7. Nếu tại thời điểm đó không có dữ liệu sẽ tiến hành chờ và liên tục pull dữ liệu sau X time



danh sách channel playout khả dụng từ: common/channel-playouts

chi tiết 1 channel playout: common/channel-playouts/${channelId}?$eager=[channelEpg, account]



mỗi channel playout sẽ liên kết 1 channel epg <-> channel epg items

mỗi channel epg item là 1 programme -> 1 video hls m3u8

channelEPgItem: common/channel-epg/${channel?.channelEpgId}/schedules?$eager=[programme,programme.programmeStreams,programme.pods]&$skip=0&$limit=30000&$date=${beforeDate.format('YYYY-MM-DD')}&tz=${TZInfo.num}



### Flow

3 modules inside:

- API:

quản lý các live playout CRUD <-> DB

- PrepareSimlive: Thực hiện prepare data cho StartSimLive

Dựa trên các playout đã đăng kí tiến hành chuẩn bị dữ liệu

-> get channel epg items

-> lấy m3u8 url

-> tách lấy playlist -> lấy ts url

-> tạo mapping table 1.ts -> ts url thực

-> luu mapping table vào redis

- StartSimlive

Dựa trên dữ liệu đã được chuẩn bị từ PrepareSimlive, tiến hành start simlive

-> dowload ts file

-> tao m3u8 playout dựa trên redis mapping




đây là mô tả yêu cầu. giúp chuyển đổi + tối ưu + thêm các phần thiếu giúp triển khai trơn tru