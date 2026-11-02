# 05 — Antarmuka Admin Incident Publisher & Manajemen Status Target

Type: grilling
Status: resolved
Blocked by: 01

## Question

Bagaimana desain antarmuka administratif di dalam dashboard Pantau untuk: (1) Mengaktifkan/menonaktifkan Public Status Page dan mengatur branding (nama sistem, deskripsi, logo, link support); (2) Memilih Host dan Desired Rule untuk di-opt-in sebagai Status Target lengkap dengan penamaan publik (*Public Display Name*); dan (3) Form alur kerja publikasi Status Incident dan riwayat pembaruan statusnya (*Status Incident Updates*)?

## Answer

### 1. Integrasi Tab Pengaturan Global (`settingsModal`)

Tab baru **`🌐 Status Page`** (`#tabSettingsStatusPage`) disematkan di dalam Action Dialog pengaturan sistem (`#settingsModal` dengan kelas `.modal-dialog-lg`).

Panel ini memuat 3 seksi kerja utama:

#### A. Seksi Konfigurasi & Branding
- **Global Toggle**: Checkbox `Enable Public Status Page` (`status_page_enabled`).
- **Live Link Banner**: Jika aktif, menampilkan tautan langsung `[↗ Buka Public Status Page (/status)]` yang dapat diklik untuk pratinjau instan di tab baru.
- **Formulir Metadata**:
  - `Site Title` (default: "Pantau Status")
  - `Organization Name` (default: nama perusahaan/tim)
  - `Header Description` (default: "Real-time service status and incident updates")
  - `Logo / Favicon URL` (opsional)
  - `Support / Helpdesk URL` (opsional, misal tautan ke portal tiket support)
- **Aksi**: Tombol `Simpan Pengaturan Status Page` yang menyimpan nilai ke tabel `settings` dan memicu invalidasi cache publik.

---

### 2. Antarmuka Manajemen Opt-In Status Target

Untuk mencegah kebocoran informasi server privat, pendaftaran target bersifat *eksplisit opt-in*:

1. **Tabel Daftar Status Target**:
   - Menampilkan kolom: `Nama Publik`, `Tipe Sumber` (Host / Desired Rule), `Kelompok (Group)`, `Urutan`, `Status` (Aktif/Nonaktif), dan `Aksi` (Edit / Hapus).
   - Dilengkapi tombol toggle cepat aktif/nonaktif per target tanpa menghapus riwayat ketersediaannya.
2. **Action Dialog Tambah/Edit Target (`#statusTargetModal`)**:
   - Kontainer modal ringkas (`.modal-dialog` ~520px).
   - **Pemilihan Tipe Target**:
     - *Tipe Host*: Dropdown memilih Host terdaftar di Pantau.
     - *Tipe Desired Rule*: Memilih Host terlebih dahulu, lalu memilih spesifik rule yang aktif (misal rule service `nginx`, port `443`, container `app_core`).
   - **Public Display Name**: Teks wajib untuk nama samaran publik (misal "REST API Gateway Production", "Customer Web Portal").
   - **Group Name**: Input teks atau pilihan grup cepat (misal: "Core Services & APIs", "Customer Web Applications", "Database Infrastructure").
   - **Sort Order**: Angka urutan tampilan.
   - **Aksi Simpan**: Validasi duplikasi via `POST /api/settings/status-page/targets`, memperbarui SQLite, dan memicu rebuild snapshot in-memory.

---

### 3. Alur Kerja Publikasi Status Incident & Incident Updates

Antarmuka publikasi insiden dirancang untuk memberikan transparansi penuh saat terjadi gangguan atau pemeliharaan sistem:

1. **Daftar Insiden Terkelola**:
   - Terbagi menjadi dua kelompok: **Insiden Aktif** (*Active / Unresolved*) dan **Arsip Insiden Selesai** (*Past / Resolved*).
   - Insiden aktif menampilkan badge status (*Investigating*, *Identified*, *Monitoring*), tingkat dampak (*Minor*, *Major*, *Critical*, atau *Scheduled Maintenance*), dan daftar target terdampak.

2. **Action Dialog Buat Insiden Baru (`#statusIncidentModal`)**:
   - **Incident Title**: Judul insiden ringkas (misal: "Investigasi Latensi Kueri Database Utama").
   - **Current Status**: Dropdown pilihan status awal (`Investigating` [default], `Identified`, `Monitoring`, `Resolved`).
   - **Impact Level**: Dropdown tingkat dampak (`None - Scheduled Maintenance`, `Minor`, `Major`, `Critical`).
   - **Affected Targets**: Checklist multi-select memilih Status Target publik yang terpengaruh gangguan.
   - **Message**: Textarea pesan penjelasan resmi kepada publik.
   - **Aksi Publikasikan**: `POST /api/settings/status-page/incidents`. Insiden langsung muncul di bagian atas halaman `/status`.

3. **Alur Kerja Pembaruan Kronologis (`#statusIncidentUpdateModal`)**:
   - Tombol **`[+ Tambah Pembaruan]`** pada kartu insiden aktif membuka modal ringkas:
     - Pilihan status terbaru: misal beralih dari `Investigating` ke `Monitoring` atau `Resolved`.
     - Textarea pesan perkembangan (misal: "Node pengganti telah online dan beban kueri kembali normal. Kami memantau stabilitas trafik.").
     - Tombol **`Kirim Pembaruan`** (`POST /api/settings/status-page/incidents/{id}/updates`):
       - Menambahkan entri berstempel waktu ke tabel `status_page_incident_updates`.
       - Jika status dipilih `Resolved`, field `resolved_at` pada insiden otomatis terisi timestamp saat ini, insiden ditutup, dan status global sistem dipulihkan kembali ke `All Systems Operational`.

---

### 4. Prinsip Desain UI (Ponytail & Zero-Overhead)

- **Komponen Native Pantau**: Seluruh antarmuka menggunakan kelas modal CSS yang sudah ada (`.modal-dialog-lg`, `.modal-dialog`, `.card`, `.btn`), memanfaatkan sistem notifikasi `showToast()`, dan dialog konfirmasi `confirmModal()` sebelum menghapus data.
- **Konsistensi i18n**: Seluruh label teks formulir dan tombol dilengkapi atribut `data-i18n` yang terdaftar pada kamus multibahasa Pantau.
- **Auto-Sync Snapshot**: Setiap operasi simpan/edit/hapus pada target maupun insiden secara otomatis memanggil channel invalidasi cache Go backend (`InvalidateStatusPageCache()`), sehingga tampilan publik di `/status` selalu terbarukan tanpa jeda.
