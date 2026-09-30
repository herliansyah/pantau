# 0027. Terminal Clipboard Bridge, Context-Aware Copy, dan Multiline Paste Safety

Integrasi penanganan papan klip (clipboard) dwiarah pada Terminal Tab Pantau melalui penyalinan kontekstual, penempelan standar peramban dengan proteksi perintah multi-baris, dan kompatibilitas penuh lingkungan HTTP non-HTTPS.

## Konteks dan Masalah

Sesi terminal interaktif Pantau berjalan di atas emulasi `xterm.js` yang terhubung via WebSocket ke PTY remote server SSH. Dalam penggunaan harian, administrator menghadapi kendala saat melakukan salin-tempel (*copy-paste*) antara terminal dan lingkungan luar:

1. **Konflik Tombol `Ctrl+C` dengan Sinyal PTY Unix (`SIGINT`)**:
   Pada sistem Unix/Linux, `Ctrl+C` mengirimkan karakter ASCII `0x03` (`SIGINT`) untuk membatalkan proses aktif (seperti `top`, `ping`, atau skrip shell). Jika peramban membajak `Ctrl+C` tanpa syarat untuk operasi *clipboard copy*, pengguna kehilangan kemampuan fundamental untuk menghentikan proses shell.
2. **Kebutuhan Navigasi Shell `Ctrl+P`**:
   Pada shell Linux (mode readline bash/zsh), `Ctrl+P` adalah pintasan default navigasi riwayat perintah sebelumnya (*previous-history*). Memetakan `Ctrl+P` ke fungsi paste akan merusak kebiasaan sysadmin dalam menelusuri riwayat perintah.
3. **Risiko Eksekusi Perintah Multi-Baris Tak Disengaja (*Command Injection / Accidental Execution*)**:
   Menempelkan teks multi-baris yang disalin dari internet atau dokumentasi (mengandung karakter baris baru `\n`) langsung mengeksekusi baris-baris perintah tersebut di terminal tanpa verifikasi pengguna.
4. **Batasan Keamanan Peramban pada HTTP Alamat IP**:
   API `navigator.clipboard.readText()` diblokir oleh browser modern pada koneksi non-HTTPS / IP publik (insecure context). Implementasi harus mendukung operasi paste melalui native DOM event agar tetap dapat berjalan pada deployment Pantau berbasis HTTP IP lokal.

## Keputusan Arsitektur

1. **Context-Aware `Ctrl+C` & `Ctrl+Shift+C`**:
   - Menggunakan `term.attachCustomKeyEventHandler`:
     - Jika pengguna sedang memilih/memblokir teks (`term.hasSelection()` bernilai `true`), penekanan `Ctrl+C` menyalin teks seleksi ke clipboard pengguna dan mencegah pengiriman `\x03` ke PTY.
     - Jika **tidak ada** teks yang dipilih, `Ctrl+C` dilewatkan ke terminal untuk mengirim sinyal `SIGINT` (`\x03`) secara normal.
   - Penekanan `Ctrl+Shift+C` selalu menyalin teks seleksi ke clipboard dan mencegah terbukanya panel Inspector / DevTools peramban bawaan Chrome/Chromium.

2. **Standar Paste `Ctrl+V` & `Ctrl+Shift+V` (Retensi `Ctrl+P`)**:
   - Menangani `Ctrl+V` dan `Ctrl+Shift+V` sebagai pemicu paste ke terminal.
   - Tombol `Ctrl+P` dibiarkan utuh ke PTY agar navigasi riwayat perintah shell (*previous history*) tetap berfungsi 100%.

3. **Proteksi Konfirmasi Perintah Multi-Baris (*Multiline Paste Confirmation*) & Opsi Perataan Baris Tunggal (*Flatten to Single Line*)**:
   - Sebelum teks ditempelkan ke PTY WebSocket, sistem memeriksa keberadaan karakter baris baru (`\n` atau `\r`).
   - Jika teks terdiri dari satu baris (*single-line*), teks langsung dialirkan ke PTY tanpa jeda.
   - Jika teks memiliki multi-baris, Pantau memunculkan modal dialog konfirmasi 3-arah (`showConfirm`):
     - **Batal (`Cancel`)**: Membatalkan penempelan dan mengembalikan fokus kursor ke terminal.
     - **🔗 Jadikan 1 Baris (`Flatten to Single Line`)**: Mengubah perintah multi-baris menjadi satu baris tunggal menggunakan `flattenToSingleLine()`. Fungsi ini membersihkan backslash line continuations (`\ + \n`), mengganti newline menjadi spasi, memadatkan spasi berlebih, dan memangkas karakter enter/spasi di ujung teks sehingga perintah hanya duduk di prompt shell tanpa langsung tereksekusi.
     - **📋 Tempel Apa Adanya (`Paste As Is`)**: Menempelkan teks multi-baris apa adanya dengan mempertahankan karakter enter / baris baru.

4. **Copy-on-Select dan Right-Click Paste**:
   - Seleksi teks menggunakan kursor mouse pada area terminal secara otomatis menyalin teks ke clipboard (*copy-on-select*).
   - Klik kanan pada area terminal memicu penempelan isi clipboard. Jika API `navigator.clipboard.readText()` diblokir oleh peramban karena diakses via HTTP IP non-HTTPS, sistem menampilkan Toast informatif yang mengarahkan pengguna menekan `Ctrl+V`.

5. **Universal Fallback untuk HTTP dan HTTPS**:
   - Operasi salin (*copy*) memanfaatkan `navigator.clipboard.writeText()` dengan fallback `document.execCommand('copy')` untuk menjamin fungsi salin pada HTTP murni.
   - Operasi tempel (*paste*) menangkap event DOM `paste` asli dari textarea pembantu `xterm` (`e.clipboardData.getData('text/plain')`), memastikan teks luar dapat ditempelkan bahkan di lingkungan HTTP tanpa sertifikat SSL.
