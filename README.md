# Docker Registry Mirror Stack

สแตกนี้เอาไว้รัน Docker Registry Mirror แบบใช้งานจริงภายในองค์กร โดยมี 3 ส่วนหลัก:

- `registry` เป็น mirror หลักสำหรับให้ Docker client ดึง image เร็ว ๆ ผ่าน `http://IP:PORT`
- `control` เป็น Web UI / API สำหรับดูสถานะ จัดการ cleanup และดู logs
- `gc-worker` เป็น worker แยกสำหรับรัน garbage collection โดยไม่ให้ control plane ถือสิทธิ์สูงเกินจำเป็น

ค่าเริ่มต้นถูกปรับให้เหมาะกับ production ภายใน:

- mirror ยังใช้แบบตรง `http://IP:PORT` ได้
- control bind แค่ `127.0.0.1` โดย default
- control ควรเปิดผ่าน HTTPS reverse proxy เท่านั้น
- secure cookie เปิดเป็น default
- `/notifications` บังคับยืนยันตัวตน
- logic event แยกตาม `push` / `pull` / `delete`
- `delete` event จะไม่ resurrect artifact กลับมาเอง
- GC ไม่ใช้ `docker.sock` ใน control plane แล้ว

คู่มือนี้เขียนให้ทำตามได้ตั้งแต่ `git clone` จนใช้งานได้จริง โดยตัดเรื่องที่ไม่จำเป็นออกให้เหลือแต่สิ่งสำคัญ

## ภาพรวมเร็ว ๆ

หลังติดตั้งเสร็จ จะได้ประมาณนี้:

- Docker client ในวงในชี้ mirror ไปที่ `http://YOUR_SERVER_IP:5000`
- ผู้ดูแลระบบเข้า Web UI ผ่าน `https://YOUR_CONTROL_HOSTNAME/login`
- ตัว `control` จริงจะฟังแค่ `127.0.0.1:8080`
- reverse proxy รับ HTTPS แล้วส่งต่อเข้า `127.0.0.1:8080`

## สิ่งที่ต้องมี

เครื่องปลายทางควรเป็น Linux และมี:

- สิทธิ์ `sudo`
- internet ออกไปติดตั้ง package และดึง image ได้
- DNS หรือ hostname ภายในสำหรับหน้า control ถ้าจะใช้ reverse proxy
- port ที่ต้องใช้:
  - `5000/tcp` สำหรับ registry mirror
  - `443/tcp` สำหรับ reverse proxy ของ control

ไม่จำเป็นต้องมี Docker ติดตั้งมาก่อน เพราะ `install.sh` จะจัดการให้

## โฟลว์ติดตั้งแบบแนะนำ

### 1. เข้าเครื่องปลายทาง

```bash
ssh your-user@YOUR_SERVER_IP
```

### 2. clone โปรเจกต์

```bash
git clone https://github.com/botnick/docker-registry-mirror-stack.git
cd docker-registry-mirror-stack
```

ถ้าจะใช้ branch อื่น:

```bash
git clone -b main https://github.com/botnick/docker-registry-mirror-stack.git
cd docker-registry-mirror-stack
```

### 3. ดูค่า config ตัวอย่างก่อน

```bash
sed -n '1,220p' .env.example
```

### 4. รัน installer

```bash
chmod +x install.sh
sudo ./install.sh
```

installer จะทำให้:

1. ตรวจ distro และ package manager
2. update/upgrade เครื่อง
3. ติดตั้ง Docker และ Compose
4. เพิ่ม user เข้า docker group ถ้าจำเป็น
5. สร้าง `.env` ถ้ายังไม่มี
6. generate `SESSION_SECRET` และ `NOTIFICATIONS_PASSWORD`
7. ถ้ายังไม่มีฐานข้อมูล จะถาม bootstrap admin ครั้งแรก
8. build และรัน `registry`, `control`, `gc-worker`

### 5. เช็กว่าสแตกขึ้นครบ

```bash
sudo docker compose ps
```

ถ้าเครื่องรองรับแค่ `docker-compose` ให้ใช้:

```bash
sudo docker-compose ps
```

## ค่า default สำคัญที่ควรรู้

ค่าใน [`.env.example`](/c:/Users/n/Downloads/registry-mirror-stack/.env.example) ที่สำคัญที่สุดมีแค่นี้:

- `REGISTRY_PORT=5000`
- `CONTROL_BIND_ADDRESS=127.0.0.1`
- `CONTROL_PORT=8080`
- `PUBLIC_BASE_URL=`
- `COOKIE_SECURE=true`
- `ALLOW_INSECURE_CONTROL=false`
- `TRUST_PROXY_HEADERS=true`
- `NOTIFICATIONS_USERNAME=registry-notify`
- `NOTIFICATIONS_PASSWORD=CHANGE_ME`

ความหมายแบบสั้น:

- `REGISTRY_PORT` คือ port ที่ Docker client ใช้ยิงเข้า mirror ตรง
- `CONTROL_BIND_ADDRESS` ควรคงเป็น `127.0.0.1`
- `PUBLIC_BASE_URL` ควรตั้งเป็น URL HTTPS ของหน้า control หลัง reverse proxy
- `COOKIE_SECURE=true` คือ session cookie จะใช้กับ HTTPS เท่านั้น
- `ALLOW_INSECURE_CONTROL=false` คือไม่ยอมให้ control วิ่งแบบ insecure โดยไม่ตั้งใจ
- `NOTIFICATIONS_*` คือ user/password ที่ registry ใช้ยิง webhook เข้า `/notifications`

## การตั้งค่าแบบแนะนำก่อนรันจริง

หลังรัน installer ครั้งแรกแล้ว แนะนำให้แก้ `.env` ให้ตรงเครื่องจริง

เปิดไฟล์:

```bash
nano .env
```

อย่างน้อยให้เช็กค่าพวกนี้:

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

ถ้าแก้ `.env` แล้วให้ build ใหม่:

```bash
sudo docker compose up -d --build
```

## วิธีใช้งานที่ถูกต้อง

### Registry mirror

ให้ใช้ตรงผ่าน IP:PORT ภายในเครือข่ายได้เลย:

```text
http://YOUR_SERVER_IP:5000
```

ตรงนี้ไม่จำเป็นต้องไปวิ่งผ่าน Cloudflare หรือ reverse proxy ถ้าเป้าหมายคือความเร็วภายใน

### Control plane

ไม่ควรเปิด `http://YOUR_SERVER_IP:8080` ตรง ๆ ให้คนใช้งาน

แนวทางที่ถูกคือ:

- `control` ฟังที่ `127.0.0.1:8080`
- reverse proxy รับ `https://YOUR_CONTROL_HOSTNAME`
- reverse proxy ส่งต่อไป `http://127.0.0.1:8080`

## ตัวอย่างตั้ง Nginx reverse proxy

ติดตั้ง Nginx:

```bash
sudo apt-get update
sudo apt-get install -y nginx
```

สร้าง config:

```bash
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
```

เปิดใช้งาน:

```bash
sudo ln -sf /etc/nginx/sites-available/registry-control.conf /etc/nginx/sites-enabled/registry-control.conf
sudo nginx -t
sudo systemctl reload nginx
```

จากนั้นตั้งใน `.env`:

```env
PUBLIC_BASE_URL=https://registry-control.internal.example
```

แล้ว rebuild:

```bash
sudo docker compose up -d --build
```

## วิธีตั้ง Docker client ให้ใช้ mirror

บนเครื่อง client ที่จะดึง image ผ่าน mirror:

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
{
  "registry-mirrors": ["http://YOUR_SERVER_IP:5000"]
}
JSON
sudo systemctl restart docker
```

เช็กว่าใช้ได้:

```bash
docker info | grep -A5 "Registry Mirrors"
```

จากนั้นลอง pull:

```bash
docker pull alpine:latest
```

## วิธีเข้าใช้งานครั้งแรก

ถ้าเป็นการติดตั้งครั้งแรก `install.sh` จะถาม:

- bootstrap username
- bootstrap password

ถ้ากด Enter ไม่ใส่ password ระบบจะ generate ให้และบังคับเปลี่ยนรหัสผ่านหลัง login ครั้งแรก

หลังทุกอย่างพร้อม:

- Registry mirror: `http://YOUR_SERVER_IP:5000`
- Web UI: `https://YOUR_CONTROL_HOSTNAME/login`
- Local health ของ control: `http://127.0.0.1:8080/healthz`

## คำสั่งใช้งานประจำ

ดู container:

```bash
sudo docker compose ps
```

ดู log control:

```bash
sudo docker compose logs -f control
```

ดู log GC worker:

```bash
sudo docker compose logs -f gc-worker
```

ดู log registry:

```bash
sudo docker compose logs -f registry
```

build/restart ใหม่:

```bash
sudo docker compose up -d --build
```

หยุด stack:

```bash
sudo docker compose down
```

เริ่มใหม่:

```bash
sudo docker compose up -d
```

## วิธีอัปเดตจาก GitHub

เข้าโฟลเดอร์โปรเจกต์แล้วรัน:

```bash
cd ~/docker-registry-mirror-stack
git pull origin main
sudo docker compose up -d --build
```

เช็กสถานะหลังอัปเดต:

```bash
sudo docker compose ps
sudo docker compose logs --tail=100 control
sudo docker compose logs --tail=100 gc-worker
sudo docker compose logs --tail=100 registry
```

## ถ้าจะปรับ policy cleanup

ค่าหลักอยู่ใน `.env`:

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

- `UNUSED_DAYS` คือไม่ค่อยได้ใช้กี่วันถึงเริ่มเข้าเกณฑ์ลบ
- `MIN_CACHE_AGE_DAYS` คือ image ใหม่เกินไปอย่าเพิ่งลบ
- `LOW_WATERMARK_PCT` คือต่ำกว่านี้เริ่มต้องคิดเรื่อง cleanup
- `TARGET_FREE_PCT` คือเป้าหมายพื้นที่ว่างหลัง cleanup
- `EMERGENCY_FREE_PCT` คือโหมดฉุกเฉิน พื้นที่ต่ำมาก
- `DRY_RUN=true` ใช้ทดสอบ policy โดยยังไม่ลบจริง

แก้เสร็จแล้วให้ apply:

```bash
sudo docker compose up -d --build
```

## สิ่งที่ระบบป้องกันให้แล้ว

- `/notifications` รับเฉพาะ request ที่ auth ผ่าน
- `push`, `pull`, `delete` event ถูกแยก logic แล้ว
- `delete` event จะ mark ลบ ไม่ชุบ artifact กลับมาเอง
- GC ทำใน `gc-worker`
- control ไม่มี `docker.sock`
- control default ไม่ bind ออก LAN

## สิ่งที่ยังควรระวังเอง

- อย่า expose `control` ออก internet ตรง ๆ
- อย่าตั้ง `COOKIE_SECURE=false` ถ้าไม่ได้จำเป็นจริง
- อย่าตั้ง `PUBLIC_BASE_URL` เป็น `http://...` เว้นแต่ตั้งใจยอมรับความเสี่ยง
- ถ้าจะเปิด control แบบไม่ secure ต้องตั้ง `ALLOW_INSECURE_CONTROL=true` เองอย่างชัดเจน
- ถ้าเป็นเครื่องหลายคนใช้ร่วมกัน ควรจำกัด firewall ให้เข้าถึง port `5000` เฉพาะวงที่ต้องใช้

## เช็กลิสต์หลังติดตั้ง

รันตามนี้ทีละบรรทัด:

```bash
cd ~/docker-registry-mirror-stack
sudo docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
sudo docker compose logs --tail=50 control
sudo docker compose logs --tail=50 gc-worker
sudo docker compose logs --tail=50 registry
```

ถ้าใช้ reverse proxy แล้ว ให้เช็กเพิ่ม:

```bash
curl -kI https://YOUR_CONTROL_HOSTNAME/login
```

ถ้า mirror เปิดให้ client ใช้งานแล้ว ให้เช็กจาก client:

```bash
docker pull alpine:latest
```

## ปัญหาที่เจอบ่อย

### เปิดหน้า control ไม่ได้

เช็ก:

```bash
sudo docker compose ps
sudo docker compose logs --tail=100 control
curl -fsS http://127.0.0.1:8080/healthz
```

### เข้า UI แล้วโดนเด้งเรื่อง cookie/origin

มักเกิดจาก `PUBLIC_BASE_URL` ไม่ตรงกับ URL จริงหลัง reverse proxy

แก้ `.env` ให้ตรง เช่น:

```env
PUBLIC_BASE_URL=https://registry-control.internal.example
```

แล้วรัน:

```bash
sudo docker compose up -d --build
```

### Docker client ไม่ใช้ mirror

เช็กไฟล์:

```bash
cat /etc/docker/daemon.json
docker info | grep -A5 "Registry Mirrors"
```

แล้ว restart Docker:

```bash
sudo systemctl restart docker
```

### อยากล้างทุกอย่างแล้วเริ่มใหม่

คำสั่งนี้จะลบ container แต่ยังไม่ลบ volume:

```bash
sudo docker compose down
```

ถ้าจะลบ data ถาวรด้วย ต้องระวังมาก:

```bash
sudo docker compose down -v
rm -rf metadata state
```

คำสั่งนี้จะทำให้ metadata และ state หาย ใช้เฉพาะตอนตั้งใจเริ่มใหม่จริง ๆ

## ไฟล์สำคัญ

- [docker-compose.yml](/c:/Users/n/Downloads/registry-mirror-stack/docker-compose.yml)
- [.env.example](/c:/Users/n/Downloads/registry-mirror-stack/.env.example)
- [install.sh](/c:/Users/n/Downloads/registry-mirror-stack/install.sh)
- [registry/config.yml](/c:/Users/n/Downloads/registry-mirror-stack/registry/config.yml)
- [app/config.go](/c:/Users/n/Downloads/registry-mirror-stack/app/config.go)
- [app/store.go](/c:/Users/n/Downloads/registry-mirror-stack/app/store.go)
- [app/ops.go](/c:/Users/n/Downloads/registry-mirror-stack/app/ops.go)
