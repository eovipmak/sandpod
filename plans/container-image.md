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
- [ ] Kiểm tra RPM fallback có thật cho mọi symlink trong `Containerfile:89-99`.
  Hiện tại: cài `nodejs,nodejs24,nodejs26` nhưng symlink `node-22`;
  cài `python3,python3.13,python3.14` nhưng symlink `python3.12`.
  Xác minh: `podman run --rm <img> ls -l /usr/local/bin/node* /usr/bin/node*`.
  Nếu thiếu: thêm `nodejs22` / `python3.12` explicit vào `dnf install`
  (hoặc ghi rõ "không fallback cho bản này" vào Spec + panel docs).
  `scope: in-scope — image customer là trách nhiệm repo (scope.md §2).`
- [ ] Xác minh placeholder `/usr/lib64/libonig.so.105` (`Containerfile:64`).
  Chạy container không mount host file → PHP-FPM phải báo lỗi rõ ràng
  (hoặc entrypoint check + exit 2), không crash mập mờ.
  `scope: in-scope — runtime share read-only từ host (scope.md §2).`
- [ ] Xác minh `__SUPERVISOR_PASSWORD__` không lọt vào image đã build
  (`podman run --rm <img> grep -r __SUPERVISOR_PASSWORD__ /etc/supervisord.conf`
  phải rỗng) + operator lấy password thế nào sau build (vault/panel?).
  Thêm CI check phía host sau build (repo-only không verify được layer).
  `scope: in-scope — image customer (scope.md §2); password end-user thuộc panel.`
- [ ] Full E2E matrix sau rebuild: 5× `php8X.test`, port apps Node/Python
  với + không shared mount, SSH key inject, reload `USR2`/`restart`,
  health-check domain thật.
  `scope: in-scope — verify mức host, làm sau khi có yêu cầu explicit.`

## 2. Verify (trên host, khi được yêu cầu)
```sh
podman build -t localhost/sandpod/customer-runtime:node-shared modules/customer-container/
podman run -d --name t1 <img>  # + biến thể mount/không mount /opt/sandpod, libonig
curl -H 'Host: php82.test' http://127.0.0.1:<port>/index.php  # x5 minors
podman exec t1 grep -c __SUPERVISOR_PASSWORD__ /etc/supervisord.conf  # phải 0
```
