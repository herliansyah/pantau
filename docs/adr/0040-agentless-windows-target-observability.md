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
   - Aturan `service`: Menggunakan `Get-Service`. Status `Running` dinormalisasi otomatis menjadi `active` (dan `Stopped` menjadi `stopped`/`inactive`) agar evaluasi aturan seragam dengan Linux.
   - Aturan `disk`: Target `/` atau kosong pada Windows dinormalisasi otomatis menjadi drive sistem `C:`, atau target drive letter spesifik (`D:`, `E:`).
   - Aturan `port`: Mengecek status port listening via `Get-NetTCPConnection` / `netstat`.
   - Aturan `process`: Memeriksa keberadaan proses aktif via `Get-Process`.
   - Aturan `backup`: Memeriksa keberadaan dan batas usia (*freshness*) file via `Get-Item`.
   - Aturan `scheduled_task`: Memeriksa status keberadaan dan kesiapan tugas terjadwal Windows via `Get-ScheduledTask`.
   - Aturan `container`: Memeriksa container Docker aktif pada Windows via `docker ps`.

5. **Injeksi Kunci SSH Otomatis (Windows OpenSSH Key Provisioning)**:
   - Pantau mengotomatisasi peletakan SSH Public Key pada Host Windows melalui `ProvisionWindowsSSHKey`.
   - Untuk akun standar non-administrator: kunci ditempatkan pada profil pengguna di `%USERPROFILE%\.ssh\authorized_keys`.
   - Untuk akun anggota grup Administrators: OpenSSH pada Windows mewajibkan penempatan kunci di `%ProgramData%\ssh\administrators_authorized_keys`.
   - Menerapkan penguncian ACL ketat via utilitas bawaan `icacls` menggunakan Universal Well-Known SIDs (`*S-1-5-18` untuk `NT AUTHORITY\SYSTEM` dan `*S-1-5-32-544` untuk `BUILTIN\Administrators`) guna menghindari kegagalan lokalisasi nama grup pada instalasi Windows multibahasa non-Inggris.

6. **Navigasi Jalur Sadar-Drive (Drive-Aware Remote Navigation)**:
   - Remote File Manager dan Terminal mendukung format drive letter Windows (`C:/`, `D:/`) dan emulasi path Git Bash (`/c/`, `/d/`) melalui helper `CleanWindowsRemotePath`.
   - Penanganan navigasi folder induk (`..`) tidak boleh melompati akar drive (`C:\`), serta pembacaan SFTP dibatasi maksimum 2MB menggunakan `io.ReadAll` dengan `LimitReader` guna mencegah pemotongan *stream*.
   - Terminal interaktif (Web Terminal dan Native Terminal Bridge) mengenali pola prompt `PS C:\...>` dan `C:\...>` untuk sinkronisasi folder aktif, serta menggunakan sintaks pergantian drive tanpa operator majemuk yang tidak kompatibel.

7. **Preset Telemetri Aplikasi & Lifecycle EOL**:
   - Menambahkan preset probe telemetri aplikasi berbasis CIM untuk target Windows: IIS Web Service (`Win32_PerfFormattedData_W3SVC_WebService`) dan Microsoft SQL Server (`Win32_PerfFormattedData_MSSQLSERVER_SQLServerGeneralStatistics`).
   - Memasukkan Windows 10 (EOL Oktober 2025) dan Windows Server 2016 (mainstream ended Januari 2022) ke dalam kalkulator *Lifecycle Longevity Assessment*.

8. **Investigasi Ruang Penyimpanan (Windows Storage Triage)**:
   - Ekstensi kapabilitas investigasi disk on-demand untuk target Windows didokumentasikan secara terperinci pada [ADR-0039 Bagian 7](0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md).

9. **Revisi Dokumen Terkait**:
   - Definisi **Host** dan istilah domain baru pada `CONTEXT.md` diperbarui untuk mencakup target Linux maupun Windows secara resmi.
   - Batasan eksklusif Linux pada ADR-0014 digantikan oleh keputusan arsitektur ini.
