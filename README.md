# Registry Mirror Stack

Registry Mirror Stack is a self-hosted Docker registry mirror for teams that want faster internal pulls, a local Docker Hub cache, a control plane for cleanup and operations, and a separate garbage collection worker for safer production use.

โปรเจกต์นี้เหมาะกับคนที่ต้องการทำ Docker Registry Mirror ภายในองค์กรให้ใช้งานจริงได้ง่ายขึ้น โดยยังคงความเร็วแบบ `IP:PORT` ตรงสำหรับฝั่ง mirror แต่แยก control plane และ garbage collection ออกมาให้ปลอดภัยกว่าเดิม

## System Preview

ภาพด้านล่างคือ control plane ของระบบจริงที่ใช้ดู mirror health, artifacts, cleanup, GC, logs, maintenance, และ runtime settings ทั้งบน desktop และ mobile โดยข้อมูลสำคัญยังแสดงครบและไม่ซ่อน digest หรือสถานะหลักทิ้ง

<table>
  <tr>
    <td width="50%">
      <img src="docs/screenshots/login.png" alt="Control plane login screen" width="100%">
      <p><strong>Login</strong><br>หน้าเข้าสู่ระบบสำหรับ operator พร้อมมุมมองรวมของ cache health, cleanup และ GC</p>
    </td>
    <td width="50%">
      <img src="docs/screenshots/dashboard-overview.png" alt="System dashboard" width="100%">
      <p><strong>Dashboard</strong><br>ภาพรวมระบบสำหรับเช็ก health, storage pressure, fallback state และงานที่ต้องตัดสินใจทันที</p>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <img src="docs/screenshots/artifact-catalog.png" alt="Artifact catalog" width="100%">
      <p><strong>Artifact Catalog</strong><br>ค้นหา repo, ดู digest แบบเต็ม, pin/protect, และเช็กสถานะของ cached images จากจอเดียว</p>
    </td>
    <td width="50%">
      <img src="docs/screenshots/cleanup-and-gc.png" alt="Cleanup workflow" width="100%">
      <p><strong>Cleanup Workflow</strong><br>ดู candidate, threshold ปัจจุบัน, และประวัติ cleanup เพื่อ reclaim storage อย่างปลอดภัย</p>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <img src="docs/screenshots/runtime-settings.png" alt="Runtime settings page" width="100%">
      <p><strong>Runtime Settings</strong><br>สรุป config runtime, cleanup policy, fallback behavior, retention, และ path สำคัญทั้งหมด</p>
    </td>
    <td width="50%">
      <img src="docs/screenshots/mobile-artifact-cards.png" alt="Responsive mobile artifact cards" width="100%">
      <p><strong>Responsive Mobile View</strong><br>หน้าจอ mobile จะจัดตารางหลักเป็น card layout เพื่อให้ข้อมูลครบ อ่านง่าย และไม่ล้น viewport</p>
    </td>
  </tr>
</table>

<details>
  <summary><strong>Complete Control Plane Preview</strong></summary>

  <table>
    <tr>
      <td width="50%"><img src="docs/screenshots/cache-overview.png" alt="Cache overview page" width="100%"><br><strong>Cache Overview</strong></td>
      <td width="50%"><img src="docs/screenshots/artifact-detail.png" alt="Artifact detail page" width="100%"><br><strong>Artifact Detail</strong></td>
    </tr>
    <tr>
      <td width="50%"><img src="docs/screenshots/events-stream.png" alt="Registry events stream" width="100%"><br><strong>Events</strong></td>
      <td width="50%"><img src="docs/screenshots/jobs-history.png" alt="Job history page" width="100%"><br><strong>Jobs</strong></td>
    </tr>
    <tr>
      <td width="50%"><img src="docs/screenshots/pinned-protected.png" alt="Pinned and protected management page" width="100%"><br><strong>Pinned and Protected</strong></td>
      <td width="50%"><img src="docs/screenshots/garbage-collection.png" alt="Garbage collection page" width="100%"><br><strong>Garbage Collection</strong></td>
    </tr>
    <tr>
      <td width="50%"><img src="docs/screenshots/fallback-upstream.png" alt="Fallback and upstream monitoring page" width="100%"><br><strong>Fallback and Upstream</strong></td>
      <td width="50%"><img src="docs/screenshots/maintenance-controls.png" alt="Maintenance controls page" width="100%"><br><strong>Maintenance Controls</strong></td>
    </tr>
    <tr>
      <td width="50%"><img src="docs/screenshots/system-logs.png" alt="System logs page" width="100%"><br><strong>System Logs</strong></td>
      <td width="50%"><img src="docs/screenshots/password-change.png" alt="Password change page" width="100%"><br><strong>Password Change</strong></td>
    </tr>
    <tr>
      <td width="50%"><img src="docs/screenshots/force-password.png" alt="First login force password change page" width="100%"><br><strong>First Login Password Rotation</strong></td>
      <td width="50%"></td>
    </tr>
  </table>
</details>

## Why This Project Exists

หลายทีมต้องการสิ่งนี้พร้อมกัน:

- Docker Hub cache เพื่อลด latency เวลา `docker pull`
- internal Docker registry mirror สำหรับเครื่อง build, CI runner, และ server ภายใน
- registry proxy cache ที่ไม่ต้องพึ่งบริการภายนอก
- cleanup และ garbage collection ที่ดูและสั่งงานได้จาก UI
- production defaults ที่ปลอดภัยกว่าแค่เปิด registry proxy ตรง ๆ

สแตกนี้จึงรวมสิ่งที่ใช้งานจริงบ่อยไว้ในชุดเดียว:

- `registry` สำหรับทำ Docker Registry Mirror และ registry proxy cache
- `control` สำหรับ Web UI / API, observability, cleanup, maintenance, และ event tracking
- `gc-worker` สำหรับ registry garbage collection แบบแยกหน้าที่ ไม่ให้ control plane ถือสิทธิ์ host มากเกินจำเป็น

## What You Get

- Fast internal Docker image pulls through a self-hosted registry router
- Docker Hub proxy cache ที่ยังใช้งานผ่าน `http://IP:PORT` ได้ตรง ๆ ภายในวงแลน
- Host-prefixed cache routes สำหรับ `ghcr.io` และ `quay.io` จาก endpoint เดียว
- Protected control plane ที่ bind แค่ `127.0.0.1` โดย default
- Secure cookie และ reverse-proxy-friendly settings สำหรับ production
- Responsive operator UI ที่ยังอ่านข้อมูลครบทั้ง desktop และ mobile
- Authenticated registry notifications
- Catalog sync จาก cached registries เพื่อให้ UI เห็น artifact ที่ถูก pull จริงแม้ webhook อย่างเดียวไม่พอ
- Artifact lifecycle logic ที่แยก `push`, `pull`, และ `delete` ชัดเจน
- Registry cleanup workflow และ garbage collection worker ที่สิทธิ์ต่ำกว่าแบบเดิม

## Architecture

ภาพรวมการใช้งานจริง:

- Docker daemon ใช้ mirror ที่ `http://YOUR_SERVER_IP:5000` สำหรับ Docker Hub
- GHCR และ Quay ใช้ router host เดียวกันแต่ใส่ registry host ไว้ใน path เช่น `YOUR_SERVER_IP:5000/ghcr.io/...`
- Operators ใช้ control UI ผ่าน `https://YOUR_CONTROL_HOSTNAME/login`
- `router` รับ request จาก client แล้วเลือก backend cache ตาม registry host
- `registry-dockerhub`, `registry-ghcr`, และ `registry-quay` ทำ pull-through cache แยกกัน
- ตัว `control` ฟังอยู่ที่ `127.0.0.1:8080`
- reverse proxy รับ HTTPS แล้วส่งต่อเข้า `127.0.0.1:8080`
- `gc-worker` อ่าน GC request แยกต่อ upstream แล้วรัน registry garbage collection แยกจาก control plane

แนวทางนี้ช่วยให้ได้ทั้ง:

- ความเร็วของ internal image cache จาก host เดียว
- ความง่ายของ Docker Hub, GHCR และ Quay cache แบบ self-hosted
- การแยกสิทธิ์ของงาน maintenance ที่เสี่ยงกว่า

## Quick Start

บนเครื่อง Linux ปลายทาง:

```bash
ssh your-user@YOUR_SERVER_IP
git clone https://github.com/botnick/docker-registry-mirror-stack.git
cd docker-registry-mirror-stack
chmod +x install.sh
sudo ./install.sh
```

ตัว installer จะ:

1. ตรวจ distro และติดตั้ง Docker / Compose
2. สร้าง `.env` จาก `.env.example` ถ้ายังไม่มี
3. generate `SESSION_SECRET` และ `NOTIFICATIONS_PASSWORD`
4. ถ้าเป็นครั้งแรก จะถาม bootstrap admin
5. build และรัน `router`, `registry-dockerhub`, `registry-ghcr`, `registry-quay`, `control`, `gc-worker`

หลังติดตั้งเสร็จ ให้เช็ก:

```bash
sudo docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
```

ถ้าเครื่องรองรับแค่ `docker-compose` ให้เปลี่ยนคำสั่งตามนั้น

## Production Setup

ค่า default รอบนี้ถูกตั้งให้เหมาะกับ production ภายใน:

- Registry router เปิดออกภายนอกเครื่องที่ `REGISTRY_PORT`
- Control plane bind แค่ `127.0.0.1`
- Control plane ควรเปิดผ่าน HTTPS reverse proxy
- Secure cookie เปิดเป็น default
- `/notifications` ต้อง auth
- GHCR และ Quay cache ถูกเปิดมาให้พร้อมใช้ตั้งแต่แรก
- GC แยกไป worker โดยไม่ใช้ `docker.sock` ใน control plane

ค่า config ที่สำคัญใน [`.env.example`](/c:/Users/n/Downloads/registry-mirror-stack/.env.example):

```env
REGISTRY_PORT=5000
CONTROL_BIND_ADDRESS=127.0.0.1
CONTROL_PORT=8080
PUBLIC_BASE_URL=
COOKIE_SECURE=true
ALLOW_INSECURE_CONTROL=false
TRUST_PROXY_HEADERS=true
NOTIFICATIONS_USERNAME=registry-notify
NOTIFICATIONS_PASSWORD=CHANGE_ME
```

หลัง installer รันเสร็จ แนะนำให้เปิด `.env` แล้วตั้งค่าจริงของระบบ:

```bash
nano .env
```

ตัวอย่างค่าแนะนำ:

```env
REGISTRY_PORT=5000
CONTROL_BIND_ADDRESS=127.0.0.1
CONTROL_PORT=8080
PUBLIC_BASE_URL=https://registry-control.internal.example
COOKIE_SECURE=true
ALLOW_INSECURE_CONTROL=false
TRUST_PROXY_HEADERS=true
NOTIFICATIONS_USERNAME=registry-notify
NOTIFICATIONS_PASSWORD=your-long-random-password
CONTROL_BOOTSTRAP_USERNAME=admin
CONTROL_BOOTSTRAP_FORCE_PASSWORD_CHANGE=true
```

ถ้าแก้ `.env` แล้ว:

```bash
sudo docker compose up -d --build
```

## Reverse Proxy for the Control Plane

Registry router ยังควรให้ Docker client ยิงตรงผ่าน `IP:PORT` เพื่อความเร็วภายใน แต่หน้า control ควรอยู่หลัง HTTPS reverse proxy

ตัวอย่าง Nginx:

```bash
sudo apt-get update
sudo apt-get install -y nginx
sudo tee /etc/nginx/sites-available/registry-control.conf >/dev/null <<'NGINX'
server {
    listen 443 ssl http2;
    server_name registry-control.internal.example;

    ssl_certificate /etc/ssl/fullchain.pem;
    ssl_certificate_key /etc/ssl/private.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
NGINX
sudo ln -sf /etc/nginx/sites-available/registry-control.conf /etc/nginx/sites-enabled/registry-control.conf
sudo nginx -t
sudo systemctl reload nginx
```

แล้วตั้ง:

```env
PUBLIC_BASE_URL=https://registry-control.internal.example
```

จากนั้น rebuild:

```bash
sudo docker compose up -d --build
```

## Direct HTTP Control UI

ถ้าคุณตั้งใจจะเปิดหน้า control ตรงแบบ `http://IP:8080` ภายในจริง ๆ ก็ทำได้ แต่ต้องปิด secure cookie ให้ตรงกับวิธีเข้าใช้งาน ไม่เช่นนั้นจะเกิดอาการ login ผ่านแต่ browser ไม่เก็บ session และหน้า `/force-password` หรือ `/dashboard` จะเข้าไม่ได้

ตั้งค่าใน `.env` แบบนี้:

```env
CONTROL_BIND_ADDRESS=0.0.0.0
CONTROL_PORT=8080
PUBLIC_BASE_URL=http://YOUR_SERVER_IP:8080
COOKIE_SECURE=false
ALLOW_INSECURE_CONTROL=true
TRUST_PROXY_HEADERS=false
```

จากนั้น rebuild:

```bash
sudo docker compose up -d --build
```

เช็กว่า cookie ไม่ได้ถูกตั้งเป็น `Secure`:

```bash
curl -i http://YOUR_SERVER_IP:8080/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"username":"admin","password":"YOUR_PASSWORD"}'
```

ถ้าจะทดสอบให้ครบแบบเก็บ cookie:

```bash
curl -c cookie.txt -i http://YOUR_SERVER_IP:8080/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"username":"admin","password":"YOUR_PASSWORD"}'

curl -b cookie.txt http://YOUR_SERVER_IP:8080/api/auth/me
```

## Configure Docker Clients to Use the Mirror

บนเครื่อง client ที่จะใช้ Docker Hub mirror ภายใน:

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
{
  "registry-mirrors": ["http://YOUR_SERVER_IP:5000"]
}
JSON
sudo systemctl restart docker
docker info | grep -A5 "Registry Mirrors"
docker pull alpine:latest
```

แนวทางนี้ทำให้ได้ private Docker Hub cache โดยไม่ต้องบังคับให้ traffic ของ mirror ไปผ่าน reverse proxy หรือ CDN

## Multi-Upstream Pulls

สำหรับ registry ที่ไม่ใช่ Docker Hub ให้เรียกผ่าน router host โดยใส่ registry host เดิมไว้ใน path:

```bash
docker pull YOUR_SERVER_IP:5000/ghcr.io/pterodactyl/yolks:java_21
docker pull YOUR_SERVER_IP:5000/ghcr.io/ptero-eggs/yolks:java_25
docker pull YOUR_SERVER_IP:5000/quay.io/pterodactyl/yolks:java_11
```

ตัวอย่างจาก Pterodactyl ที่แปลงมาใช้ mirror:

```text
ghcr.io/pterodactyl/yolks:java_21
=> YOUR_SERVER_IP:5000/ghcr.io/pterodactyl/yolks:java_21

ghcr.io/ptero-eggs/yolks:debian
=> YOUR_SERVER_IP:5000/ghcr.io/ptero-eggs/yolks:debian

quay.io/pterodactyl/yolks:java_8
=> YOUR_SERVER_IP:5000/quay.io/pterodactyl/yolks:java_8
```

หลัง pull สำเร็จ control plane จะ import catalog ของ cache เข้ามาเองตอน startup และตามรอบ sync จึงควรเห็น artifact ในหน้า UI แม้ pull-through proxy บางกรณีจะไม่ได้ส่ง webhook ครบทุกจังหวะ

ถ้าต้องการให้รายการขึ้นทันทีหลัง pull ให้เปิดหน้า `Cache Overview` แล้วกด `Sync catalog now`

## Pterodactyl

ถ้าจะใช้กับ Pterodactyl หรือ Wings:

- Docker Hub image ปกติยังใช้ daemon mirror ได้เหมือนเดิม
- GHCR และ Quay ให้เปลี่ยน image name ใน egg หรือ yolk เป็น `YOUR_SERVER_IP:5000/<registry-host>/<repo>:<tag>`
- ตัวอย่างเช่น `ghcr.io/pterodactyl/yolks:java_17` ให้เปลี่ยนเป็น `YOUR_SERVER_IP:5000/ghcr.io/pterodactyl/yolks:java_17`
- เมื่อเปลี่ยนแล้ว pull จะวิ่งผ่าน cache backend ที่ตรงกับ registry host โดยอัตโนมัติ

## First Login

ถ้าเป็นการติดตั้งครั้งแรก `install.sh` จะถาม:

- bootstrap username
- bootstrap password

ถ้ากด Enter ตอน password ระบบจะ generate password ให้ และบังคับเปลี่ยนรหัสหลัง login ครั้งแรก

จุดเข้าใช้งานหลัก:

- Registry mirror: `http://YOUR_SERVER_IP:5000`
- Control UI: `https://YOUR_CONTROL_HOSTNAME/login`
- Local health check: `http://127.0.0.1:8080/healthz`

## Daily Operations

ดูสถานะบริการ:

```bash
sudo docker compose ps
```

ดู log ของ control plane:

```bash
sudo docker compose logs -f control
```

ดู log ของ garbage collection worker:

```bash
sudo docker compose logs -f gc-worker
```

ดู log ของ router และ registry caches:

```bash
sudo docker compose logs -f router
sudo docker compose logs -f registry-dockerhub
sudo docker compose logs -f registry-ghcr
sudo docker compose logs -f registry-quay
```

build หรือ restart ใหม่:

```bash
sudo docker compose up -d --build
```

หยุด stack:

```bash
sudo docker compose down
```

เริ่ม stack อีกครั้ง:

```bash
sudo docker compose up -d
```

## Updating from GitHub

```bash
cd ~/docker-registry-mirror-stack
git pull origin main
sudo docker compose up -d --build
sudo docker compose ps
sudo docker compose logs --tail=100 control
sudo docker compose logs --tail=100 gc-worker
sudo docker compose logs --tail=100 router
sudo docker compose logs --tail=100 registry-dockerhub
sudo docker compose logs --tail=100 registry-ghcr
sudo docker compose logs --tail=100 registry-quay
```

## Cleanup and Garbage Collection Policy

ค่าที่ใช้บ่อยใน `.env`:

```env
DRY_RUN=false
JANITOR_INTERVAL_SECONDS=3600
MAX_DELETE_BATCH=20
UNUSED_DAYS=30
MIN_CACHE_AGE_DAYS=3
LOW_WATERMARK_PCT=20
TARGET_FREE_PCT=35
EMERGENCY_FREE_PCT=10
GC_HOUR_UTC=19
```

ความหมายแบบใช้งานจริง:

- `UNUSED_DAYS` คือ artifact ไม่ค่อยได้ใช้กี่วันถึงเริ่มเข้าเกณฑ์ลบ
- `MIN_CACHE_AGE_DAYS` คือ artifact ใหม่เกินไปอย่าเพิ่งลบ
- `LOW_WATERMARK_PCT` คือระดับพื้นที่ว่างที่เริ่มต้องคิดเรื่อง cleanup
- `TARGET_FREE_PCT` คือเป้าหมายพื้นที่ว่างหลัง cleanup
- `EMERGENCY_FREE_PCT` คือโหมด storage emergency
- `DRY_RUN=true` ใช้ทดสอบ registry cleanup policy โดยยังไม่ลบจริง

ถ้าปรับค่าแล้ว:

```bash
sudo docker compose up -d --build
```

## Common Use Cases

โปรเจกต์นี้เหมาะกับกรณีแบบนี้:

- ต้องการ Docker Registry Mirror สำหรับ office network หรือ private datacenter
- ต้องการ Docker Hub cache สำหรับ CI/CD runners
- ต้องการใช้ image ของ Pterodactyl จาก `ghcr.io` หรือ `quay.io` ผ่าน cache ภายใน
- ต้องการ self-hosted container image cache แทนการ pull ออก internet ตรงทุกครั้ง
- ต้องการ internal registry proxy cache ที่มี cleanup, logs, maintenance, GC workflow และหน้า control รวม
- ต้องการ private Docker mirror ที่ยังเร็วแบบ `IP:PORT` แต่ฝั่ง control ปลอดภัยขึ้น

## Operational Safety

สิ่งที่ระบบช่วยป้องกันให้แล้ว:

- `/notifications` รับเฉพาะ authenticated request
- `push`, `pull`, และ `delete` event ถูกแยก logic ชัดเจน
- `delete` event จะ mark ลบ ไม่ resurrect artifact
- GC ทำใน `gc-worker`
- Control plane ไม่มี `docker.sock`
- Control plane default ไม่ bind ออก LAN

สิ่งที่ยังควรระวังเอง:

- อย่า expose `control` ออก internet ตรง ๆ
- อย่าปิด `COOKIE_SECURE` ถ้าไม่ได้จำเป็นจริง
- อย่าตั้ง `PUBLIC_BASE_URL` เป็น `http://...` ถ้าไม่ได้ตั้งใจยอมรับความเสี่ยง
- ถ้าจะเปิด control แบบ insecure ต้องตั้ง `ALLOW_INSECURE_CONTROL=true` เองอย่างชัดเจน
- ควรจำกัด firewall ของ port `5000` ให้ตรงวง client ที่ต้องใช้จริง

## Verification Checklist

หลังติดตั้งหรือหลังอัปเดต ให้รัน:

```bash
cd ~/docker-registry-mirror-stack
sudo docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
sudo docker compose logs --tail=50 control
sudo docker compose logs --tail=50 gc-worker
sudo docker compose logs --tail=50 router
sudo docker compose logs --tail=50 registry-dockerhub
sudo docker compose logs --tail=50 registry-ghcr
sudo docker compose logs --tail=50 registry-quay
```

ถ้ามี reverse proxy:

```bash
curl -kI https://YOUR_CONTROL_HOSTNAME/login
```

ถ้ามี Docker client ที่ตั้ง mirror แล้ว:

```bash
docker pull alpine:latest
```

ถ้าจะเช็กเส้นทาง GHCR และ Quay:

```bash
docker pull YOUR_SERVER_IP:5000/ghcr.io/pterodactyl/yolks:java_21
docker pull YOUR_SERVER_IP:5000/quay.io/pterodactyl/yolks:java_11
```

## Troubleshooting

### Control UI เปิดไม่ได้

```bash
sudo docker compose ps
sudo docker compose logs --tail=100 control
curl -fsS http://127.0.0.1:8080/healthz
```

### Login แล้วเจอปัญหา cookie หรือ origin

มักเกิดจาก `PUBLIC_BASE_URL` ไม่ตรงกับ URL จริงหลัง reverse proxy

ตั้งให้ตรง เช่น:

```env
PUBLIC_BASE_URL=https://registry-control.internal.example
```

แล้วรัน:

```bash
sudo docker compose up -d --build
```

### Login ผ่านแต่หน้าไม่ไปต่อ

อาการนี้มักเกิดตอนเปิด control ผ่าน `http://IP:8080` ตรง ๆ แต่ยังใช้ค่าเดิมแบบ production:

```env
COOKIE_SECURE=true
ALLOW_INSECURE_CONTROL=false
```

ถ้าจะใช้ direct HTTP จริง ให้เปลี่ยนเป็น:

```env
PUBLIC_BASE_URL=http://YOUR_SERVER_IP:8080
COOKIE_SECURE=false
ALLOW_INSECURE_CONTROL=true
TRUST_PROXY_HEADERS=false
```

แล้วรัน:

```bash
sudo docker compose up -d --build
```

### Docker client ไม่ใช้ registry mirror

```bash
cat /etc/docker/daemon.json
docker info | grep -A5 "Registry Mirrors"
sudo systemctl restart docker
```

### Control plane ยังไม่เห็น artifact หลัง pull

เช็กก่อนว่าการ pull ผ่าน router จริง:

```bash
sudo docker compose logs --tail=100 router
sudo docker compose logs --tail=100 registry-dockerhub
sudo docker compose logs --tail=100 registry-ghcr
sudo docker compose logs --tail=100 registry-quay
```

กรณี `docker pull hello-world:latest`:

- ถ้าใช้ `registry-mirrors` แล้ว Docker Hub ควรวิ่งผ่าน `YOUR_SERVER_IP:5000`
- ถ้า log Docker บอก `trying next host after status: 404 Not Found` แปลว่า mirror route หรือ backend ยังไม่พร้อม และ Docker fallback ไปดึงตรงจาก upstream

กรณี `ghcr.io/...` หรือ `quay.io/...`:

- อย่า pull ด้วยชื่อเดิมตรง ๆ ถ้าต้องการให้ผ่าน cache
- ให้ใช้รูปแบบ `YOUR_SERVER_IP:5000/ghcr.io/...` หรือ `YOUR_SERVER_IP:5000/quay.io/...`

หลัง pull สำเร็จ control plane จะ import catalog ของ cache ตอน startup และตามรอบ sync ดังนั้น artifact ควรเริ่มปรากฏใน UI หลังจากนั้นไม่นาน

ถ้าต้องการบังคับ sync ทันที ให้เปิดหน้า `Cache Overview` แล้วกด `Sync catalog now`

### อยากล้างแล้วเริ่มใหม่

หยุด stack ก่อน:

```bash
sudo docker compose down
```

ถ้าจะลบ data ถาวรด้วย ใช้อย่างระวัง:

```bash
sudo docker compose down -v
rm -rf metadata state
```

คำสั่งนี้จะลบ metadata และ state ที่เก็บไว้ ใช้เฉพาะตอนตั้งใจ reset จริง ๆ

## Control UI Visibility

README อธิบายภาพรวมของระบบ, วิธีติดตั้ง, และกรณีใช้งานที่พบบ่อยให้ครบในภาษาที่อ่านง่ายตามการใช้งานจริง

ส่วน control UI ถูกตั้งให้ส่ง `noindex` และ `robots.txt` แบบ block ไว้แล้ว เพราะเป็นหน้า operator ภายในที่ควรเข้าถึงผ่านลิงก์ตรงและสิทธิ์ที่เหมาะสมเท่านั้น

## Important Files

- [README.md](/c:/Users/n/Downloads/registry-mirror-stack/README.md)
- [docker-compose.yml](/c:/Users/n/Downloads/registry-mirror-stack/docker-compose.yml)
- [.env.example](/c:/Users/n/Downloads/registry-mirror-stack/.env.example)
- [install.sh](/c:/Users/n/Downloads/registry-mirror-stack/install.sh)
- [registry/dockerhub.yml](/c:/Users/n/Downloads/registry-mirror-stack/registry/dockerhub.yml)
- [registry/ghcr.yml](/c:/Users/n/Downloads/registry-mirror-stack/registry/ghcr.yml)
- [registry/quay.yml](/c:/Users/n/Downloads/registry-mirror-stack/registry/quay.yml)
- [app/config.go](/c:/Users/n/Downloads/registry-mirror-stack/app/config.go)
- [app/store.go](/c:/Users/n/Downloads/registry-mirror-stack/app/store.go)
- [app/ops.go](/c:/Users/n/Downloads/registry-mirror-stack/app/ops.go)
