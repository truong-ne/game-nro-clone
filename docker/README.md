# Chạy thử Teamobi 2026 bằng Docker

Yêu cầu: Docker Desktop đang chạy với Linux containers.

## Sử dụng trên Windows

- Bật server: mở `start.cmd`.
- Xem log: mở `logs.cmd`; nhấn Ctrl+C để ngừng xem log.
- Tắt container: mở `stop.cmd` (nên thoát hết client trước).
- Client Mod Local chạy trên Windows, kết nối `127.0.0.1`, cổng `14445`.

Server không phải website, không truy cập bằng trình duyệt. Hiện chỉ mở cổng trên máy này.

Hoặc chạy trong PowerShell tại thư mục gốc Teamobi2026:

```powershell
docker compose -f docker/compose.yaml up -d --build
docker compose -f docker/compose.yaml ps
docker compose -f docker/compose.yaml logs -f --tail 100 game
docker compose -f docker/compose.yaml stop
```

## Cấu hình

- Java 21 chạy bản có sẵn `SRC/20.jar`; không biên dịch lại source.
- MariaDB 10.4.32, cùng phiên bản ghi trong file SQL đi kèm.
- Database `team2026` tự import khi volume được tạo lần đầu. Biến `DB_INIT_SQL=./database-current.sql` trong `.env` chọn bản sao database mới nhất; nếu không có biến này thì dùng file gốc `database team2026.sql`.
- Dữ liệu database lưu trong volume `teamobi2026-local_database`, được giữ khi stop/start hoặc recreate container.
- `Config.properties` ở thư mục docker là bản cấu hình riêng: host database `db`, tài khoản `teamobi`, IP client `127.0.0.1`.
- `.env` chứa mật khẩu database được tạo riêng. Theo yêu cầu chuyển máy, bản sao Git có kèm file này và `Config.properties`.
- Không sửa các file Java, JAR hoặc cấu hình Windows trong SRC.
- Dockerfile tạo thêm bản `data/map/tile_set_info` từ `tile_set_Info` bên trong image để tương thích tên file trên Linux.
- Bản thử nghiệm không tự khởi động lại container game khi ứng dụng tự thoát; mở lại bằng `start.cmd`.

Không dùng `docker compose down -v` nếu muốn giữ dữ liệu database. File SQL chỉ import tự động ở lần khởi tạo đầu tiên; sửa file SQL trên Windows không tự cập nhật database đang dùng.

## Phạm vi đã kiểm tra

Đã khởi động database, import 41 bảng, tải dữ liệu game, mở cổng 14445 và nhận phản hồi bắt tay giao thức NRO (command -27).

Chưa kiểm tra đăng nhập và chơi bằng Mod Local. Các lệnh gọi `.bat` của ứng dụng Windows không chạy được trong Linux; bản thử nghiệm dùng Docker để quản lý việc bật/tắt container.
