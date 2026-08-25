## 🇻🇳 Phiên bản Tiếng Việt: Đặc tả & Kiến trúc dự án SimLive

### 1. Cơ sở hạ tầng (Infrastructure)

* **Ngôn ngữ/Framework:** Golang với Gin Gonic (API)
* **Database:** PostgreSQL (Lưu trữ metadata, cấu hình channel) + **GORM** (ORM)
* **Cache/State:** **Redis** (Lưu trữ mapping TS, session, state của luồng live) *(Mới bổ sung)*
* **Công cụ xử lý Media:** FFmpeg, TSDuck, FFprobe (Thực thi qua Go `os/exec`)
* **Job/Queue:** Asynq hoặc Go-cron (Quản lý các task chạy ngầm lập lịch) *(Mới bổ sung)*
* **Deployment:** Docker & `docker-compose.yml` (App, Postgres, Redis)

### 2. Tối ưu & Các tính năng bổ sung (Critical Additions)

Để hệ thống chạy 24/7 trơn tru, đây là những phần đã được thêm vào so với yêu cầu gốc của bạn:

* **Cơ chế Fallback (Slate Video):** Nếu API trả về rỗng hoặc lỗi khi tải `.ts`, hệ thống không được dừng. Nó phải phát một video chờ (Slate/B-roll) vòng lặp để giữ kết nối HLS không bị đứt.
* **Sliding Window Download (Tải cuốn chiếu):** Không tải toàn bộ TS của 1 ngày cùng lúc. Chỉ tải trước 3-5 segments tiếp theo dựa trên thời gian thực để tiết kiệm băng thông và ổ cứng.
* **Garbage Collection (Dọn rác):** Tự động xóa các file `.ts` cũ đã phát và xóa key trên Redis để tránh tràn ổ đĩa (Disk full).
* **Worker Pool:** Quản lý số lượng goroutine tải file `.ts` đồng thời để không làm sập server khi có quá nhiều channel chạy cùng lúc.

### 3. Luồng hoạt động chi tiết (Flow)

#### Module 1: Core API & Management

* Cung cấp các RESTful endpoints để CRUD các Live Playout (Tạo, Sửa, Xóa, Bật/Tắt channel).
* Lưu trữ cấu hình channel vào PostgreSQL.
* Cung cấp endpoint trả về file `master.m3u8` và `playlist.m3u8` cho client (player).

#### Module 2: PrepareSimlive (Trình chuẩn bị dữ liệu)

*Chạy ngầm (Cron job) định kỳ (VD: mỗi 30 phút) để chuẩn bị dữ liệu cho vài giờ tiếp theo.*

* **Pull Data:** Quét các channel đang active, gọi API EPG (với `$eager=[programme...]` và giới hạn limit/date).
* **Fetch & Parse:** Lấy HLS m3u8 URL của từng programme -> Tải playlist về -> bóc tách lấy các `.ts` URL gốc.
* **Mapping:** Đánh lại số thứ tự ID liên tục (VD: `channel1_seq_1.ts`, `channel1_seq_2.ts`).
* **Store:** Lưu bảng mapping (`channel1_seq_1.ts` -> `https://origin-server/real-path/segment.ts`) và metadata (thời lượng, EXTINF) vào **Redis**.
* **Wait & Retry:** Nếu không có lịch EPG, đưa channel vào trạng thái *Waiting* và kiểm tra lại sau `X` phút.

#### Module 3: StartSimlive (Trình phát sóng & HLS Packager)

*Vòng lặp vĩnh cửu (Infinite Loop) chịu trách nhiệm tạo luồng live.*

* **Manifest Generator:** Liên tục tạo/cập nhật file `live.m3u8` (trên memory hoặc Redis) mô phỏng live window (chứa 3-5 segments gần nhất) và thăng tiến sequence number.
* **Segment Downloader (Worker Pool):** Đọc Redis mapping, tải trước các file `.ts` vào thư mục tạm (Temp Directory) hoặc xử lý trực tiếp qua stream (pipe) để tối ưu I/O.
* **Media Processing:** Nếu cần, dùng FFmpeg/TSDuck chuẩn hóa các file `.ts` (đồng nhất bitrate, resolution) trước khi serve ra ngoài để tránh player bị giật (glitch).
* **Cleanup:** Xóa các file `.ts` cũ ra khỏi thư mục tạm sau khi chúng rớt khỏi cửa sổ live của file `m3u8`.
