# 02 — Ephemeral SSH Key Injection & Auto-Cleanup Architecture

Type: research
Status: resolved
Blocked by:

## Question

Bagaimana merancang modul `Ephemeral Key Injector` di Go yang menghasilkan keypair SSH ED25519 berumur pendek (TTL 5 menit), menyuntikkan public key berlabel unik (`pantau-ephemeral-<id>-<expiry>`) ke `~/.ssh/authorized_keys` pada Host target via sesi SSH aktif Pantau, dan memastikan pembersihan otomatis (auto-cleanup / garbage collection) kunci tersebut baik setelah sesi berakhir maupun jika proses terhenti tiba-tiba?

## Answer

Riset dan arsitektur lengkap didokumentasikan di `docs/research/02-ephemeral-key-injector.md` (branch `research/02-ephemeral-key-injector`, commit `3210077`).

1. **Pembuatan Keypair In-Memory di Go**:
   - Menghasilkan ED25519 keypair secara in-memory menggunakan `crypto/ed25519` dan `golang.org/x/crypto/ssh` (`ssh.MarshalAuthorizedKey` dan `ssh.MarshalPrivateKey`) tanpa pernah menyentuh disk Pantau server.
   - Waktu pembuatan sangat cepat (~20 mikrodetik) dengan ukuran kunci 256-bit yang ringkas.

2. **Injeksi Atomik & Kompatibilitas Legacy**:
   - Dieksekusi melalui runner SSH Pantau yang sudah terautentikasi (`runner.ExecTimeout`) dalam satu round-trip (20–40ms).
   - Menggunakan format komentar `ssh-ed25519 <key> pantau-ephemeral-<id>-<expiry>` (menghindari opsi `expiry-time` OpenSSH 7.7+ yang ditolak oleh daemon SSH legacy seperti CentOS 6/7).
   - Menegakkan permissions `chmod 700 ~/.ssh` dan `chmod 600 ~/.ssh/authorized_keys` (StrictModes).
   - Penanganan trailing newline aman agar tidak merusak entri kunci administrator yang sudah ada.
   - Validasi regex input sebelum eksekusi untuk mencegah *command injection*.

3. **Arsitektur Pembersihan Dua Tingkat (Two-Tier Cleanup)**:
   - **Tier 1 (Proactive Go Timer)**: `time.AfterFunc` (default 5 menit) atau saat terminal ditutup, menghapus kunci spesifik berdasarkan Session ID (`pantau-ephemeral-<id>-*`). Kebal terhadap drift waktu (clock drift).
   - **Tier 2 (Reactive On-Host Sweep)**: Dijalankan sebelum injeksi baru atau saat inspeksi berkala untuk membersihkan kunci orphan jika Pantau mati mendadak, dengan grace period 10 menit (`now - 600`).
   - Sifat OpenSSH: Kunci hanya divalidasi saat *handshake* awal; penghapusan kunci ephemeral setelah terminal terbuka **tidak akan memutus** sesi shell yang sedang aktif.
   - Menggunakan POSIX `awk` filter dengan atomic swap (`mv -f .tmp.$$ authorized_keys`) sehingga kebal dari jebakan exit code `grep -v` dan aman dari pembacaan paralel.
   - Diisolasi dengan per-host `sync.Mutex` di memori Pantau.
