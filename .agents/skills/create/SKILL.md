---
name: create
description: >-
  Menjalankan orkestrasi 3 subagent (auditor -> developer -> qa) untuk membangun
  fitur baru atau perubahan kompleks secara hemat token ketika user memicu /create.
---

# Workflow `/create` (Multi-Agent Feature Builder)

Gunakan prosedur ini **hanya** saat user memanggil `/create <deskripsi fitur/task>` atau meminta siklus multi-agent secara eksplisit.

## Prinsip Hemat Token

- **Targetkan path & baris**: Sebelum memanggil subagent, identifikasikan file dan rentang baris yang relevan saja (jangan melakukan full-repo scan).
- **Reuse sesi aktif**: Jika subagent sudah aktif dalam konteks tugas yang sama, gunakan `send_message` alih-alih membuat subagent baru.
- **Jangan mengubah di luar scope**: Pertahankan struktur project, interface, dan perilaku existing.

## Tahapan Wajib

### 1. Phase 1 — Audit (`auditor`)
- Panggil subagent `auditor` (`invoke_subagent`) dengan deskripsi fitur dan daftar path file target absolut.
- Tunggu temuan arsitektur, concurrency, security, dan risiko yang perlu diantisipasi sebelum menulis kode.
- `auditor` dilarang mengedit file.

### 2. Phase 2 — Implementasi (`developer`)
- Kirimkan kebutuhan fitur + hasil audit dari `auditor` ke subagent `developer`.
- `developer` mengimplementasikan kode + unit test yang relevan, lalu menjalankan `go vet ./...`, `go test ./...`, dan `go build ./...` hanya pada modul yang terdampak.

### 3. Phase 3 — Independent QA (`qa`)
- Panggil subagent `qa` untuk memverifikasi `git diff`, memastikan tidak ada regresi/race/deadlock, dan menjalankan ulang `go vet ./...`, `go test -count=1 ./...`, dan `go build ./...` pada modul terkait.
- Jika `qa` melaporkan `FAIL`, kirim temuan ke `developer` untuk diperbaiki lalu uji ulang via `qa` hingga `PASS`.

### 4. Phase 4 — Final Report
Laporkan ke user secara ringkas:
1. Temuan penting dari `auditor`.
2. Ringkasan implementasi & file yang berubah (link clickable).
3. Bukti hasil `go vet`, `go test`, dan `go build` dari `qa`.
4. Risiko yang tersisa (jika ada).
