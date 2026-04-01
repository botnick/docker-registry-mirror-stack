# Docker Registry Mirror Stack

สแตกสำหรับรัน Docker Registry Mirror พร้อม Web UI / Control Plane โดยแยกงานควบคุมกับงาน GC ออกจากกันให้พร้อมใช้งานจริงมากขึ้น

ค่าเริ่มต้นรอบนี้ตั้งใจให้เหมาะกับ production ภายในองค์กร:

- Registry mirror ยังใช้งานแบบตรง `http://IP:PORT` ได้เพื่อความเร็วในวงใน
- Control plane bind แค่ `127.0.0.1` โดย default และควรเปิดผ่าน HTTPS reverse proxy เท่านั้น
- secure cookie เปิดเป็น default
- `/notifications` ถูกบังคับให้ยืนยันตัวตนด้วย Basic Auth ระหว่าง registry กับ control
- GC ย้ายไป `gc-worker` ที่ไม่มี `docker.sock` แล้ว ลดอำนาจของ control plane ต่อ host
- event ถูกแยก logic ตาม `push` / `pull` / `delete` และ `delete` event จะไม่ resurrect artifact

## ติดตั้ง

บนเครื่อง Linux ที่มีโปรเจกต์นี้อยู่แล้ว:

```bash
chmod +x install.sh
sudo ./install.sh
```

installer จะทำให้:

1. ตรวจ distro และติดตั้ง Docker/Compose
2. สร้าง `.env` จาก `.env.example` ถ้ายังไม่มี
3. generate `SESSION_SECRET` และ `NOTIFICATIONS_PASSWORD`
4. bootstrap admin ครั้งแรก
5. รัน `registry`, `control`, และ `gc-worker`

## พฤติกรรม production เริ่มต้น

- Mirror เปิดออกภายนอกเครื่องที่ `REGISTRY_PORT` ตามปกติ
- Control เปิดที่ `127.0.0.1:${CONTROL_PORT}` เท่านั้น
- ถ้าจะใช้งาน UI/API จากเครื่องอื่น ให้เอา reverse proxy ที่มี HTTPS มาครอบก่อน
- ถ้าจำเป็นต้องเปิด control แบบไม่ secure จริง ๆ ต้องตั้ง `ALLOW_INSECURE_CONTROL=true` เอง

## ตัวแปรสำคัญ

ใน [`.env.example`](/c:/Users/n/Downloads/registry-mirror-stack/.env.example):

- `CONTROL_BIND_ADDRESS=127.0.0.1`
- `COOKIE_SECURE=true`
- `ALLOW_INSECURE_CONTROL=false`
- `TRUST_PROXY_HEADERS=true`
- `NOTIFICATIONS_USERNAME=registry-notify`
- `NOTIFICATIONS_PASSWORD=...`
- `GC_WORKER_POLL_SECONDS=15`

## การเข้าใช้งาน

- Registry mirror: `http://YOUR_SERVER_IP:5000`
- Control health ภายในเครื่อง: `http://127.0.0.1:8080/healthz`
- Web UI หลัง reverse proxy: `https://YOUR_CONTROL_HOSTNAME/login`

ถ้า `PUBLIC_BASE_URL` ถูกตั้งไว้ ระบบจะใช้ค่านั้นตรวจ origin/cookie สำหรับ reverse proxy

## ตัวอย่าง reverse proxy

ตัวอย่าง Nginx สำหรับ control plane:

```nginx
server {
    listen 443 ssl http2;
    server_name registry-control.internal.example;

    ssl_certificate /etc/ssl/fullchain.pem;
    ssl_certificate_key /etc/ssl/private.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

จากนั้นตั้ง:

```env
PUBLIC_BASE_URL=https://registry-control.internal.example
```

## ตั้ง Docker client ให้ใช้ mirror ตรง

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
{
  "registry-mirrors": ["http://YOUR_SERVER_IP:5000"]
}
JSON
sudo systemctl restart docker
```

แนวทางนี้ยังใช้ mirror แบบ `ip:port` ตรงได้ตามเดิม ไม่บังคับให้ registry traffic วิ่งผ่าน Cloudflare หรือ SSL ภายนอก

## คำสั่งที่ใช้บ่อย

```bash
sudo docker compose ps
sudo docker compose logs -f control
sudo docker compose logs -f gc-worker
sudo docker compose logs -f registry
sudo docker compose up -d --build
```

ถ้าเครื่องใช้ `docker-compose` แทน `docker compose` ให้เปลี่ยนคำสั่งตามนั้น

## หมายเหตุด้านความปลอดภัย

- อย่า expose `control` ออก internet ตรง ๆ
- ถึงจะเป็นระบบภายใน `/notifications` ก็ไม่ควรเปิดรับ unauthenticated
- ถ้าจะปิด secure cookie หรือใช้ `PUBLIC_BASE_URL` แบบ `http://...` ต้องตั้ง `ALLOW_INSECURE_CONTROL=true` เองโดยชัดเจน
- GC ตอนนี้ทำผ่าน `gc-worker` ที่สิทธิ์น้อยกว่าเดิม แต่ยังควรหลีกเลี่ยงการทำ maintenance หนักช่วงโหลดสูง
