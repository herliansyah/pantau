# 0040. Agentless Windows Target Observability via Native OpenSSH and PowerShell

Menambahkan kapabilitas pemantauan dan verifikasi kondisi seharusnya (*Desired State*) pada target server maupun workstation berbasis sistem operasi Windows (Windows Server 2019/2022/2025, Windows 10/11) secara murni *agentless* melalui protokol SSH bawaan (*Native OpenSSH Server*) dan eksekusi batch PowerShell ber-encode Base64 tanpa menambah ketergantungan (*dependency*) library baru.

## Konteks dan Masalah

Sebelumnya pada [ADR-0014](0014-windows-portable-server-and-posix-remote-paths.md), cakupan pemantauan target Pantau dibatasi secara eksklusif pada sistem operasi Linux. Namun dalam infrastruktur riil, organisasi kerap mengelola lingkungan *hybrid* yang melibatkan Windows Server (Active Directory, IIS, SQL Server, failover nodes) maupun workstation/server Windows non-server.

Tantangan teknis utama dalam mendukung target Windows tanpa merusak arsitektur Pantau:
1. **Risiko Bloat Dependency**:
   - Protokol bawaan Windows lama seperti WinRM (WS-Management/SOAP via HTTP/HTTPS) membutuhkan library pihak ketiga yang besar di Go, port firewall khusus (5985/5986), serta manajemen sertifikat yang rumit.
2. **Prinsip Nol Agen (Zero-Agent)**:
   - Pantau mengusung filosofi *zero-agent*. Memasang daemon latar belakang di target Windows melanggar prinsip desain inti.
3. **Variasi Default SSH Shell**:
   - Fitur native OpenSSH Server pada Windows modern dapat menggunakan `cmd.exe` atau `powershell.exe` sebagai *DefaultShell*. Eksekusi perintah majemuk yang melibatkan tanda kutip ganda, *pipe*, atau karakter khusus rentan mengalami galat *escaping*.

## Keputusan Arsitektur

1. **Pemanfaatan Native OpenSSH Server**:
   - Pantau memanfaatkan fitur OpenSSH Server (`sshd`) yang sudah menjadi kapabilitas bawaan Windows modern (Windows 10 build 1809+, Windows Server 2019+).
   - Seluruh koneksi remote tetap menggunakan modul `internal/sshrunner` dan SFTP bawaan tanpa menambah library eksternal baru.

2. **Deteksi Sistem Operasi Hibrida (In-Band OS Probe)**:
   - Kolom `os_type TEXT DEFAULT 'linux'` ditambahkan ke tabel `hosts` dengan pembaruan idempotent.
   - Jika `os_type` masih kosong atau belum terdeteksi saat inspeksi pertama, Pantau menjalankan probe in-band mandiri tanpa operator majemuk `||` (menjaga kompatibilitas penuh dengan Windows PowerShell 5.1):
     Probe `uname -s`, dan jika tidak cocok dicoba fallback `cmd.exe /c "ver"`.
   - Target dengan respons mengandung `Windows` atau `Microsoft` diklasifikasikan sebagai `windows`, sedangkan POSIX/UNIX dicatat sebagai `linux`. Hasil deteksi langsung dicatat ke basis data sehingga inspeksi berkala berikutnya tidak memerlukan probe ulang.

3. **Eksekusi Batch PowerShell Mandiri Ber-encode Base64 (`-EncodedCommand`)**:
   - Seluruh pengumpulan metrik sistem Windows dirangkum dalam satu batch script PowerShell yang dikonversi ke format UTF-16LE dan di-encode ke Base64 via helper `powershellEncodedCmd()`.
   - Pola ini kebal terhadap perbedaan shell default (`cmd.exe` vs `powershell.exe`) dan mencegah masalah *quoting* atau *newline mangling* pada SSH session.
   - Script batch mengekstrak metrik inti: OS & Build (`Win32_OperatingSystem`), Uptime & CPU Load % (`Win32_Processor`), RAM fisik terpakai/total, kapasitas drive disk, adapter network RX/TX, koneksi TCP listening, serta top proses memori dan CPU.

4. **Normalisasi Evaluator Aturan Kondisi Seharusnya (Desired State Rules)**:
   - Aturan `service`: Menggunakan `Get-Service`. Status `Running` dinormalisasi otomatis menjadi `active` agar aturan seragam dengan Linux.
   - Aturan `disk`: Target `/` atau kosong pada Windows dinormalisasi otomatis menjadi drive sistem `C:`.
   - Aturan `port`: Mengecek status port listening via `Get-NetTCPConnection` / `netstat`.
   - Aturan `process`: Memeriksa keberadaan proses aktif via `Get-Process`.
   - Aturan `backup`: Memeriksa keberadaan dan batas usia (*freshness*) file via `Get-Item`.

5. **Revisi Dokumen Terkait**:
   - Definisi **Host** pada `CONTEXT.md` diperbarui untuk mencakup target Linux maupun Windows secara resmi.
   - Batasan eksklusif Linux pada ADR-0014 digantikan oleh keputusan arsitektur ini.
