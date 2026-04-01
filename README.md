# Docker Registry Mirror Stack

Docker Registry Mirror พร้อม Web UI และ Control Plane สำหรับติดตั้งบน Linux แบบใช้ `install.sh` ตัวเดียว เน้นเอาไปวางบนเครื่องปลายทางแล้วรันได้เลย

## ติดตั้ง

ในเครื่อง Linux ที่มีไฟล์โปรเจกต์นี้อยู่แล้ว ให้รัน:

```bash
chmod +x install.sh
sudo ./install.sh
```

สคริปต์จะทำให้:

1. ตรวจ package manager อัตโนมัติ
2. update/upgrade ระบบ
3. ติดตั้ง Docker และ Compose
4. เพิ่ม user ปัจจุบันเข้า `docker` group
5. สร้าง `.env` จาก `.env.example`
6. สร้าง `SESSION_SECRET`
7. ถ้ายังไม่มีฐานข้อมูล จะถามรหัส admin ครั้งแรก
8. รัน stack ให้เสร็จ

## รองรับ

- `apt`
- `dnf`
- `yum`
- `zypper`
- `pacman`
- `apk`

## ใช้งานหลังติดตั้ง

installer จะสรุป URL ให้ตอนจบ โดยหลักจะเป็น:

- Registry mirror: `http://YOUR_SERVER_IP:5000`
- Web UI: `http://YOUR_SERVER_IP:8080/login`
- Health: `http://YOUR_SERVER_IP:8080/healthz`

## คำสั่งที่ใช้บ่อย

ถ้าเครื่องใช้ `docker compose`:

```bash
sudo docker compose ps
sudo docker compose logs -f control
sudo docker compose logs -f registry
sudo docker compose up -d --build
```

ถ้าเครื่องใช้ `docker-compose`:

```bash
sudo docker-compose ps
sudo docker-compose logs -f control
sudo docker-compose logs -f registry
sudo docker-compose up -d --build
```

## ตั้ง Docker Client ให้ใช้ Mirror

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json >/dev/null <<'JSON'
{
  "registry-mirrors": ["http://YOUR_SERVER_IP:5000"]
}
JSON
sudo systemctl restart docker
```

ถ้าเครื่อง client ไม่ได้ใช้ `systemd` ให้เปลี่ยนคำสั่ง restart ตาม service manager ของเครื่องนั้น

## หมายเหตุ

- ถ้าเพิ่งถูกเพิ่มเข้า `docker` group ให้ reconnect SSH 1 รอบก่อนใช้ `docker` แบบไม่ใส่ `sudo`
- ถ้ารัน `down -v` จะลบข้อมูล persistent ของ stack นี้
- ค่าต่าง ๆ ปรับได้ใน `.env`
