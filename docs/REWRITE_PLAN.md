# Kế hoạch viết lại: Godot + Go + PostgreSQL

Mục tiêu: thay server Java (`SRC/`, ~103k dòng, 548 file) và client Unity đã biên dịch (`ModLocal/`) bằng:

- **`server/`**: server Go viết lại theo mô hình actor.
- **`client/`**: client Godot 4.
- **PostgreSQL**: schema chuẩn hoá.

Source Java được giữ nguyên để tham chiếu luật game.

Các quyết định đã chốt:

- Dùng **protocol mới**, nên client ModLocal cũ không dùng được với server Go.
- Chỉ **migrate dữ liệu game (template)**; tài khoản và nhân vật bắt đầu lại từ đầu.
- Code nằm **trong repo này**, trên branch `rewrite/godot-go`.

## Kiến trúc

```
Godot client ──WebSocket JSON {t,d}──► nro-server (Go)
      │                                   ├─ World: online sessions, chọn zone
      └──HTTP /api/data, /assets/*────►   ├─ Zone actor: 1 goroutine/khu, inbox + tick 100ms
                                          ├─ store: pgx → PostgreSQL
                                          └─ data: template nạp 1 lần vào RAM (~80ms)
nro-migrate: dump MariaDB (.sql) + SRC/data/map/* ──► PostgreSQL
nro-bot:     client headless để smoke/load test
```

So với bản Java, các điểm đã cải thiện:

| Vấn đề ở bản cũ | Bản mới |
|---|---|
| Mỗi người chơi 1 thread và 2 thread socket. Người chơi bị update 2 lần/giây. `ArrayList` không khoá bị nhiều thread ghi | Mỗi zone 1 goroutine sở hữu toàn bộ state, không cần khoá |
| Mật khẩu lưu plaintext, so sánh trực tiếp trong SQL | bcrypt, chống dò tài khoản qua timing, khoá IP 1 phút sau 5 lần sai |
| Server tin toàn bộ toạ độ client gửi | Kiểm tra tốc độ di chuyển và giới hạn map, gửi `pos` để kéo client về |
| Không autosave, crash là mất hết | Autosave mỗi 60s (khi có thay đổi) và save khi thoát hoặc shutdown |
| Khi mọi zone đều đầy, chọn zone lặp vô hạn | Chọn zone ít người nhất |
| Lệnh admin khớp theo `startsWith`, chat thường cũng kích hoạt | Lệnh admin bắt đầu bằng `/`, khớp chính xác, có ghi log |
| Dữ liệu là JSON lồng 3 lớp nằm trong cột TEXT | Bảng chuẩn hoá: `character_item`, `character_skill`... |
| `tile_set_info` sai hoa/thường nên chỉ chạy trên Windows | Đọc đúng tên file |
| Chat/đổi khu không giới hạn tần suất | Cooldown chat, đổi khu 5s, giới hạn 40 gói/giây |

## Milestone

**M1: Nền tảng (đã xong ở lượt này)**

- [x] Schema Postgres cho toàn bộ 30 bảng template, cùng account, character, item, skill.
- [x] `nro-migrate`: parser đọc file dump MariaDB, chuẩn hoá JSON bị encode hai lần, đọc tile map, tileset và bg item.
- [x] Server: đăng ký và đăng nhập, tạo nhân vật (bộ đồ khởi đầu theo hành tinh như bản gốc), vào map, chọn zone, di chuyển, waypoint chuyển map, đổi khu, chat khu vực, lệnh admin `/map`, `/pos`, `/online`.
- [x] HTTP `/api/data` (bundle template cho client) và `/assets/*` (chỉ các thư mục ảnh, chặn path traversal).
- [x] Unit test cho parser, bộ chuẩn hoá, tạo nhân vật, stat, ground, chọn zone và chống teleport; bot test end-to-end.
- [x] Client Godot: đăng nhập, tạo nhân vật, vẽ tile map, bg item, NPC, quái (frame đầu), nhân vật (head/body/leg), đi, nhảy, trọng lực, waypoint, đổi khu, chat, HUD.
- [ ] Chạy thử client trên Godot (máy chưa cài Godot).

**M2: Hoàn thiện hiển thị (đã xong phần code; client chưa chạy thử trên Godot)**

- [x] Tool `nro-render`: vẽ map, part và quái ra PNG từ asset gốc, để kiểm chứng quy tắc hiển thị khi chưa có Godot.
- [x] Bảng animation nhân vật 14 frame (đứng, chạy 5 frame, nhảy, rơi, bay, đánh 3 frame), dựng từ bảng slot part đã render. `CharInfo` gốc nằm trong mã máy IL2CPP nên không trích ra được. Server gửi bảng này trong `/api/data`.
- [x] Đồng bộ hướng và trạng thái animation qua mạng.
- [x] Nền parallax: `b{bg}{layer}` cộng hậu tố `-{bgType}` nếu có, layer 3 xa nhất. Các layer neo theo đường mặt đất phổ biến nhất của map, màu trời lấy từ layer xa nhất.
- [x] Toạ độ bg item tính theo ô 24px (đã kiểm chứng bằng ảnh render).
- [x] Quái: server parse sẵn (binary, dạng text của mob 70, file có byte id thừa của mob 92/93) và trả về `/api/mob/{id}` kèm sprite sheet. Client chạy animation theo sequence.
- [x] Va chạm tường (flag 4/8), bay (nhấn nhảy khi đang trên không), trừ 1% KI tối đa mỗi giây. Server ép trạng thái "rơi" khi hết KI.
- [ ] Mob 70: kích thước ảnh con phải suy ra từ sprite sheet nên các mảnh còn lệch nhẹ. Mob 71 và 118 vẽ chưa đúng. Các mob 90, 91, 109–116 không có file x2.
- [ ] Hiệu ứng thời tiết, layer bg item phía trước nhân vật, độ lệch frame bay: kiểm tra lại khi chạy client.

Tối ưu đã làm trong M2:

- Gộp di chuyển: mỗi tick 100ms gửi một gói `moves` cho cả khu, thay cho một gói cho mỗi lần di chuyển của mỗi người.
- Khu không có người thì không tick (trước đây khoảng 1.700 khu tick 10 lần/giây).
- Map không còn gửi qua WebSocket mỗi lần vào. Client tải `/api/map/{id}?v=hash` (gzip, cache vĩnh viễn) và lưu lên đĩa.
- Quái được server parse và nén gzip một lần rồi cache; client cũng cache.
- Client vẽ tile bằng `TileMapLayer` với atlas ghép lúc chạy (engine tự culling và batching), thay cho vẽ lại toàn bộ tile mỗi khi một ảnh tải xong.

**M3–M8: Lõi gameplay (đã xong phần lõi; xem "Chưa port" bên dưới)**

Mỗi tính năng đăng ký lệnh của mình vào registry `handlers`, thay cho `Controller` với khoảng 97 case. Nội dung game nằm trong [`server/internal/game/content.json`](../server/internal/game/content.json): đặt `CONTENT_FILE` để sửa nội dung mà không cần build lại.

| Milestone | Đã làm | File |
|---|---|---|
| M3 Chỉ số, chiến đấu | Bảng option → chỉ số (một bảng thay cho 3 switch bị copy). HP/KI/sức đánh theo % và cộng thẳng, giáp tối đa 86%, né tối đa 90%, xuyên giáp, phản đòn, hút HP. Cộng điểm theo công thức gốc (sửa bug cộng giáp chỉ tính tiền 1 điểm). 10 bậc giới hạn sức mạnh. Skill: cooldown, 3 kiểu tiêu KI, tầm đánh, đánh lan (`max_fight`). AI quái: aggro, quái chủ động từ id 19, hồi sinh 3s, hồi 10% HP mỗi 30s, không bị hạ bằng 1 đòn khi đầy máu, mộc nhân mỗi đòn tối đa 10%. Tiềm năng, chết và hồi sinh (1 ngọc tại chỗ hoặc về nhà miễn phí, bất tử 1,5s sau hồi sinh), hồi 1%/s khi ngoài giao tranh | `stats.go`, `combat.go`, `mob.go` |
| M4 Item, shop | Túi đồ/rương/đồ đang mặc, mặc, tháo và hoán đổi, xếp chồng. Đậu thần. Sách skill tự map theo tên "X lv N". Đồ rơi thuộc người hạ quái trong 15s và mất sau 60s. Bảng drop theo dữ liệu. Shop mở qua NPC (783 món), bán lại với giá 1/4. Giao dịch hai bên khoá rồi hoán đổi nguyên tử | `items.go`, `shop.go`, `trade.go` |
| M5 NPC, nhiệm vụ | Menu NPC tự thêm mục shop từ DB. Cây đậu thần (mọc, thu hoạch, nâng cấp). Giftcode. Nhiệm vụ chính: tự suy ra trigger từ tên nhiệm vụ con ("Đánh N con X", "Thu thập", "Báo cáo với %2", "Đi đến") kèm ghi đè trong `content.json`, nhiệm vụ 4/5/6 tách theo hành tinh. Item nhiệm vụ chỉ rơi cho người đang làm. Event bus thay cho các lời gọi `checkDone…` rải rác. Thành tựu | `npc.go`, `tasks.go`, `events.go` |
| M6 Xã hội | Đệ tử (đi theo, bảo vệ, tấn công, về nhà; tăng sức mạnh theo sát thương gây ra). Bang hội (lập, mời, vào, rời, đuổi, giải tán khi bang chủ rời, chat bang). Bạn bè, tin nhắn riêng, chat thế giới (yêu cầu sức mạnh và ngọc, cooldown) | `pet.go`, `social.go` |
| M7 Boss, phó bản | Một state machine boss chung cấu hình bằng dữ liệu (xuất hiện ngẫu nhiên trên map cấu hình, đuổi đánh, rơi thưởng, hẹn giờ hồi, tự rời map sau 10 phút), ngoại hình lấy từ sprite quái. Phó bản: zone riêng cho mỗi bang hoặc người chơi, quái HP×N không hồi, boss ở map cuối, hết giờ thì đưa mọi người về nhà và đóng zone | `boss.go`, `dungeon.go` |
| M8 PvP, vận hành | Thách đấu (chỉ đánh nhau được khi cả hai đồng ý hoặc ở map PK). Sự kiện theo lịch (ví dụ x2 EXP cuối tuần). Admin API có token và audit log: thống kê, thông báo, kick, ban, tạo giftcode, cấp item, gọi boss. Lệnh GM trong game khớp chính xác. `/metrics` dạng Prometheus. `compose.prod.yaml` với Caddy (HTTPS/WSS tự động) | `dungeon.go`, `world.go`, `server/admin.go` |

Kiểm thử:

- **Unit test**: 9 test gameplay mới (`gameplay_test.go`).
- **Bot kịch bản E2E**: `docker compose --profile tools run --rm bot -scenario <tài khoản GM>`. Bot đăng nhập, dịch chuyển, hạ quái, tăng sức mạnh, mở menu NPC và shop, mua, cộng điểm, gọi boss, ăn đậu, đánh boss, xem nhiệm vụ. Cả 12 bước đã pass.
- **Load test**: 50 bot chạy cùng lúc, không lỗi.
- **Client Godot headless**: dùng `client/Dockerfile.check`. Lệnh `-e SMOKE_HOST=...` cho client tự đăng nhập và chơi, đã pass.

### Đợt 2: port toàn bộ phần còn thiếu

Mỗi tính năng là một nhóm file riêng, cắm vào lõi qua các hook trong `hooks.go` và qua plugin client `client/scripts/features/*.gd`:

- **`buffs_*`**: hiệu ứng skill đặc biệt và buff.
  - Choáng, ngủ, trói, mù, khiên, biến khỉ, Quả cầu kênh khí, Makankosappo, tự phát nổ, Super Kame, liên hoàn, trị thương, huýt sáo, đẻ trứng.
  - Item buff có thời hạn, bùa, nội tại (mở/đổi ở các NPC ông nội), thẻ radar.
- **`items2_*`**: kinh tế item.
  - Nâng cấp, pha lê hoá, ép sao, bông tai Porata, phân rã, các công thức nhập.
  - Thỏi vàng và hộp quà, cờ và thú cưỡi.
  - Drop theo 17 nhóm map của `Mob.getItemMobReward`.
  - Vòng quay may mắn, ký gửi (bảng `consign_item`), mở giới hạn sức mạnh.
- **`prog_*`**: nhiệm vụ và tiến trình.
  - Cấu hình chính xác cho nhiệm vụ chính 7–29.
  - Nhiệm vụ phụ (Bò Mộng), nhiệm vụ bang, danh hiệu, 20 thành tựu.
  - Rồng thần với đủ các trang điều ước, rồng Namek.
- **`arena_*`**: phó bản và đấu trường.
  - 39 nhóm boss lấy từ `BossesData`, có chuỗi biến hình (Fide, Xên, Cooler…).
  - Bản Đồ Kho Báu, Con Đường Rắn Độc, Khí Gas, Doanh Trại đầy đủ, Mabư 14h, Ngọc Rồng Sao Đen.
  - Đại hội võ thuật, Siêu hạng, bảng xếp hạng, minigame Con số may mắn và Chọn ai đây.

Kiểm thử đợt 2:

- Khoảng 38 test mới (cộng với các test cũ), tất cả đều pass.
- Bot kịch bản E2E mở rộng lên 20 bước, gọi cả các hệ thống mới; pass.
- 50 bot chạy cùng lúc, không lỗi.
- Client parse sạch và smoke test pass với cả 4 plugin.

### Đợt 3: sửa lõi và hoàn thiện (đang làm dở)

Đã xong và kiểm chứng (unit test, bot kịch bản 20 bước, smoke test client):

- **Nguồn sát thương**: hook phòng thủ (`inDamageHooks`) nhận `DamageSrc{Mob, Player, Boss}`. Bùa da trâu và bất tử chỉ chặn đòn từ quái/boss, đúng như bản gốc.
- **An toàn dữ liệu**: `World.async` luôn chạy callback, kể cả khi người chơi đã thoát. Lần lưu cuối chờ cho tới khi mọi thao tác DB đang dở (`Player.pending`) xong, nên tiền hoàn ký gửi và đồ giao không còn bị mất. Zone phó bản đã đóng vẫn chạy nốt callback.
- **Trạng thái server lưu bền**: bảng `server_state` và `registerPersistent`. Lượt vào phó bản trong ngày và tiền cược minigame được lưu mỗi 30 giây, khi tắt server, và nạp lại khi khởi động.
- **Hook khi người chơi chết** (`deathHooks`), `Event.BossRef`.
- **Túi đồ** mở rộng được theo từng nhân vật (`Extra.BagExtra`, tối đa 100 ô).
- **Boss mang trang phục nhân vật**: thêm trường `BossTemplate.Parts` = [head, body, leg]. Client vẽ boss có `parts` như nhân vật, có thanh máu và chọn mục tiêu được.
- **Client**: minimap (phím M) và hiệu ứng hình ảnh dựng bằng code (tia lửa khi trúng đòn, choáng, ngủ, khiên, biến khỉ, tụ KI, nổ), trong `client/scripts/features/_minimap.gd` và `_fx.gd`.

Chưa xong (agent bị dừng giữa chừng, còn phải làm tiếp):

- **Đệ tử và trang bị**:
  - Hợp thể, skill và đồ cho đệ tử, điều ước cho đệ tử, bông tai hợp thể.
  - Chỉ số set kích hoạt, cường hoá lỗ, đánh bóng sao pha lê, đồ thiên sứ, chuyển hoá, hộp thần linh.
  - Thể lực (kèm bùa oai hùng và dẻo dai), danh sách kẻ thù và trả thù (ẩn danh), item mở rộng túi đồ.
  - Đã có 2 file nháp `server/internal/game/pg_content.json` và `pg_data.go`, gắn build tag `wip_pets` nên không nằm trong bản build.
- **Sự kiện thế giới**: dữ liệu trang phục cho từng boss (để điền `Parts`), thử thách Whis, Đại hội lần thứ 23, bot giả lập người chơi, bẫy/cờ/buff x3-x5-x7 của Ngọc Rồng Sao Đen, săn ngọc Namek trên map. Chưa có file nào.

### Chưa port hoặc còn giới hạn

- **Chưa có code**: các mục "Chưa xong" của đợt 3 ở trên (đệ tử và trang bị, sự kiện thế giới, trang phục boss).
- **Buff**: khiên vẫn chặn sát thương từ mọi nguồn; bùa oai hùng, dẻo dai và ẩn danh mới chạy đồng hồ, chưa có tác dụng (chờ phần thể lực và kẻ thù).
- **Rồng thần**: chưa có điều ước cho đệ tử.
- **Ngoại hình**: boss vẫn dùng sprite quái cho tới khi có dữ liệu `Parts`.
- **Cân bằng game**: một số giá trị lệch chủ ý so với code gốc vì code gốc có bug, ví dụ nhiệm vụ 11.0 và 27.0, hệ số bùa trí tuệ. Chi tiết ghi trong comment tại `prog_*` và `buffs_*`.
- **Client**: giao diện dựng bằng code, hiệu ứng skill là hình vẽ đơn giản (chưa dùng sprite effect gốc), chưa có âm thanh. Đã mở bằng Godot 4.4.1 trên Windows nhưng chưa ai chơi thử đầy đủ.
## Chạy thử

```bash
cd server
docker compose up -d postgres
docker compose --profile tools run --rm --build migrate   # tạo schema và import template
docker compose up -d --build gameserver                   # server tại 127.0.0.1:8765
docker compose --profile tools run --rm bot -n 5          # smoke test
```

Client: mở thư mục `client/` bằng Godot 4.4 trở lên, nhấn F5. Máy chủ mặc định là `127.0.0.1:8765`.

- Đi trái/phải: ←/→ hoặc A/D.
- Nhảy: ↑ hoặc W. Nhấn lần nữa khi đang trên không để bay.
- Đánh: J hoặc Space. Chọn mục tiêu: Tab. Chọn skill: phím 1–8.
- Nói chuyện với NPC: E. Nhặt đồ: Z (đi qua là tự nhặt).
- Bảng: I túi đồ, T nhiệm vụ, P cộng điểm, C bang hội, F bạn bè. Esc để đóng.
- Chat: Enter. Gõ `/w` để chat thế giới, `/c` để chat bang.
- Đi vào ô vàng (waypoint) để sang map khác.

Lệnh GM (tài khoản có `is_admin`): `/map id`, `/pos`, `/online`, `/item id [sl]`, `/gold n`, `/gem n`, `/power n`, `/pet`, `/boss id`, `/learn`.

Admin API (cần biến `ADMIN_TOKEN`):

```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:8765/admin/stats
```

Các endpoint POST: `/admin/broadcast`, `/admin/kick`, `/admin/ban`, `/admin/giftcode`, `/admin/give`, `/admin/boss`.

Kiểm tra client không cần cài Godot:

```bash
docker build -f client/Dockerfile.check -t nro-godot-check client
docker run --rm -v "$PWD/client:/client" nro-godot-check
docker run --rm --network nro-go_default -e SMOKE_HOST=gameserver:8765 -v "$PWD/client:/client" nro-godot-check
```

Production: xem `server/compose.prod.yaml` và `server/.env.example`.

Tài khoản admin: sau khi đăng ký, đặt quyền bằng lệnh:

```bash
docker compose exec postgres psql -U nro -d nro -c "update account set is_admin=true where username='TEN'"
```

## Định dạng dữ liệu cũ (tham khảo)

- `tile_map_data/<map>`: `u8 w, u8 h, w*h byte` theo hàng; 0 là trống, còn lại là chỉ số tile (bắt đầu từ 1), ô 24px.
- `tile_set_Info`: `u8 n; mỗi tileset: u8 nType; mỗi type: i32 flag, u8 k, k×u8 tile`. Flag 2 là mặt đất.
- `item_bg_map_data/<map>`: `i16 n; n×{i16 id, i16 x, i16 y}`.
- `mob/x*/<id>`: `u8 type, i32 len, data, i32 len, png`. Phần data gồm ảnh con, frame và sequence (big-endian).
- Ảnh đều là PNG thường, không mã hoá. Client mới dùng bộ ảnh `x2`.
