# Plan: Container image rebuild (host action — dời lại, ngoài repo-only)

> Lý do dời: mọi verify đều cần thực thi trên host (podman build, chạy container
> với/không mount, `podman exec`). Cấm làm khi chưa được yêu cầu explicit
> (rule `docs/plan.md` #1: không build/provision/restart thật).
> Mỗi bullet cần 1 dòng `scope:` theo `docs/scope.md` §4 trước khi code.

## 0. Baseline cần giữ
- Tag hiện tại `localhost/sandpod/customer-runtime:pilot` đang phục vụ customer `001`.
  Mọi thay đổi làm trên image tag mới (vd `customer-runtime:node-shared`), giữ tag
  `pilot` cho khách cũ (rule `docs/plan.md` Pha 2, mục "Thứ tự thực hiện" #2).

## 1. Việc làm (trên host, có E2E)
- [x] Kiểm tra RPM fallback có thật cho mọi symlink trong `Containerfile:89-99`.
  DONE `2026-11-23` trên host: mọi symlink `node/node-22..26/npm/npm-22..26`
  resolve fallback RPM (`node-22 --version` → v22.23.4, `npm-24` → 11.19.1,
  `python3.13/3.14` → 3.13.18/3.14.10); `command -v` đủ 12/12 tên khi không
  mount. Kết luận audit repo-only đúng: không thiếu gói (`nodejs`=22.x,
  `python3`=3.12), đã chú thích + thêm `npm-22`.
  `scope: in-scope — image customer là trách nhiệm repo (scope.md §2).`
  Hiện tại: cài `nodejs,nodejs24,nodejs26` nhưng symlink `node-22`;
  cài `python3,python3.13,python3.14` nhưng symlink `python3.12`.
  Xác minh: `podman run --rm <img> ls -l /usr/local/bin/node* /usr/bin/node*`.
  Nếu thiếu: thêm `nodejs22` / `python3.12` explicit vào `dnf install`
  (hoặc ghi rõ "không fallback cho bản này" vào Spec + panel docs).
  Repo-only `2026-11-23`: `nodejs-1:22.x` CHÍNH là Node 22 (AlmaLinux 10 không
  có gói `nodejs22`), `python3-3.12.x` CHÍNH là Python 3.12 — không thiếu gói,
  chỉ thiếu chú thích → đã chú thích trong `Containerfile`, thêm symlink
  `npm-22` còn thiếu, thêm `scripts/check-runtime-mounts` (verify fixture
  `/tmp` 2 chế độ); host `podman run` verify vẫn pending.
  `scope: in-scope — image customer là trách nhiệm repo (scope.md §2).`
- [x] Xác minh placeholder `/usr/lib64/libonig.so.105` (`Containerfile:64`).
  DONE `2026-11-23`: container không mount → 5 FPM `FATAL can't find command`,
  nginx/sshd/crond vẫn RUNNING, static 200, php → 502 rõ ràng (không crash
  mập mờ); có mount host file → FPM chạy. `check-runtime-mounts` WARN/FAIL
  đúng cả 2 trạng thái.
  `scope: in-scope — runtime share read-only từ host (scope.md §2).`
  Chạy container không mount host file → PHP-FPM phải báo lỗi rõ ràng
  (hoặc entrypoint check + exit 2), không crash mập mờ.
  Repo-only `2026-11-23`: đã chú thích placeholder + `check-runtime-mounts`
  WARN khi file rỗng / FAIL khi mất hẳn; host no-mount run vẫn pending.
  `scope: in-scope — runtime share read-only từ host (scope.md §2).`
- [x] Xác minh `__SUPERVISOR_PASSWORD__` không lọt vào image đã build
  DONE `2026-11-23`: `grep -c` trong image mới = 0 (đã substitute ngẫu nhiên
  lúc build); `sandpod-check-runtime-mounts` FAIL nếu còn placeholder.
  Ghi nhận operator: password supervisor nằm trong image đã build — lấy qua
  `podman run --rm <img> grep` từ bên trong không khả thi (file 0600, cần
  exec as root); quy trình vault/panel chính thức thuộc panel (ngoài scope).
  `scope: in-scope — image customer (scope.md §2); password end-user thuộc panel.`
  (`podman run --rm <img> grep -r __SUPERVISOR_PASSWORD__ /etc/supervisord.conf`
  phải rỗng) + operator lấy password thế nào sau build (vault/panel?).
  Thêm CI check phía host sau build (repo-only không verify được layer).
  Repo-only `2026-11-23`: `check-runtime-mounts` FAIL khi còn placeholder
  (chạy `--root` fixture lẫn trong image: `podman run --rm <img>
  sandpod-check-runtime-mounts`); quy trình operator lấy password + CI host
  vẫn pending.
  `scope: in-scope — image customer (scope.md §2); password end-user thuộc panel.`
- [x] Full E2E matrix sau rebuild: DONE `2026-11-23` (tag `node-shared`,
  `pilot` giữ nguyên; host có Remi 82-85 lúc test, 86 bổ sung sau
  (`install-php-runtimes.sh` lại, Remi đã có `php86-8.6.0`), `/opt/sandpod`
  + node/python trees cài `2026-11-23`):
  5× route → 8.2.36/8.3.37/8.4.28/8.5.13 đúng version, 86 → 502 + FPM FATAL rõ
  nguyên nhân (host thiếu runtime); Node RPM fallback chạy port-app 3001 OK
  (`node-app v22.23.4`); SSH key inject → login `customer` exit 0 (cổng
  22201/18001 bận bởi pilot nên E2E dùng 22203/18082);
  `sandpod-runtime-reload 8.3` (config-test + `USR2`) exit 0, vẫn phục vụ
  8.3.37 sau reload; bare-container static 200 + php 502. Container E2E đã
  `rm -f` dọn sạch.
  Ghi nhận thêm `2026-11-23`: image `:pilot` đang chạy (001/002) không có
  program `php86-fpm` và helper `sandpod-runtime-reload` cũ (tới 8.5) —
  KH muốn 8.6 phải migrate image sang `:node-shared` (quyết định thuộc
  panel, không tự migrate ở đây).
  `scope: in-scope — verify mức host, làm sau khi có yêu cầu explicit.`
  với + không shared mount, SSH key inject, reload `USR2`/`restart`,
  health-check domain thật. (Nguyên si host-only, chưa đụng.)
  `scope: in-scope — verify mức host, làm sau khi có yêu cầu explicit.`

## 2. Verify (trên host, khi được yêu cầu)
```sh
podman build -t localhost/sandpod/customer-runtime:node-shared modules/customer-container/
podman run -d --name t1 <img>  # + biến thể mount/không mount /opt/sandpod, libonig
curl -H 'Host: php82.test' http://127.0.0.1:<port>/index.php  # x5 minors
podman exec t1 grep -c __SUPERVISOR_PASSWORD__ /etc/supervisord.conf  # phải 0
```
