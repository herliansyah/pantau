# 03 — Remote Web Launcher Script & One-Click Command Generator

Type: grilling
Status: resolved
Blocked by: 02

## Question

Bagaimana arsitektur pengiriman skrip peluncur sekali pakai (`.cmd` untuk Windows, `.command` / `.sh` untuk macOS/Linux) dan modal One-Click Copy Command saat Pantau diakses melalui peramban jarak jauh (remote web mode), termasuk pengamanan token download sekali pakai (single-use token), penanganan private key sementara pada komputer klien, dan penghapusan otomatis file sementara setelah terminal diluncurkan?

## Answer

1. **Mekanisme Tiket Sekali Pakai (*Single-Use Burn-After-Read Token*)**:
   - Web klien meminta tiket via `POST /api/terminal/native-ticket` dengan cookie sesi aktif.
   - Backend Go membuat token kriptografis 32-byte acak (`crypto/rand`) dengan masa berlaku ketat **60 detik**.
   - Pasangan kunci ED25519 dibuat in-memory, public key diinjeksi ke Host target, dan private key disimpan sementara di cache RAM ber-TTL 60 detik.
   - Akses pengunduhan via `GET /api/terminal/native-download?ticket=<token>&os=<win|mac|linux>` langsung menghapus (*burn-after-read*) entri dari RAM server seketika, mencegah *replay attack*.

2. **Skrip Mandiri Sekali Pakai & Self-Deleting (*Zero Client Artifact*)**:
   - **Windows (`.cmd`)**: Menyematkan blok PEM private key, diekstrak ke `%TEMP%\pantau_<id>.key` dengan penguncian ACL khusus user aktif (`icacls "%KEYFILE%" /inheritance:r /grant:r "%USERNAME%:(R)"` agar lolos OpenSSH StrictModes), menjalankan `ssh -i "%KEYFILE%" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -p %PORT% %USER%@%HOST%`, lalu otomatis menghapus file kunci dan file `.cmd` itu sendiri (`del "%~f0"`).
   - **macOS (`.command`) & Linux (`.sh`)**: Menggunakan `trap 'rm -f "$KEYFILE" "$0"' EXIT INT TERM` untuk menjamin file private key di `mktemp` (`chmod 600`) dan berkas script launcher langsung terhapus seketika saat SSH selesai, gagal, atau di-interupsi (`Ctrl+C`).

3. **Modal One-Click Copy Command Streaming**:
   - Sebagai alternatif bagi pengguna yang menonaktifkan unduhan otomatis:
     - Linux/macOS: `curl -sSL https://<pantau-url>/api/terminal/native-run?ticket=<token> | bash`
     - Windows PowerShell: `irm https://<pantau-url>/api/terminal/native-run?ticket=<token> | iex`
   - Endpoint `/api/terminal/native-run` menyajikan skrip ber-TTL yang sama dengan proteksi single-use.

4. **Validasi Batas Kepercayaan (*Trust Boundary Validation*)**:
   - Hostname, username, dan port divalidasi ketat dengan regex sebelum diinterpolasi ke dalam skrip untuk mencegah injeksi baris perintah (*command injection*).
