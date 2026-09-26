# Hardware Commission Date Override pada Lifecycle Assessment

Penyediaan opsi penetapan manual tanggal komisioning hardware (`Hardware Commission Date`) pada entitas Host untuk mengakomodasi unit server stok lama (*New Old Stock* / refurbished) tanpa merusak integritas audit jejak firmware BIOS asli.

## Konteks dan Masalah

Sesuai [ADR-0012](0012-transparent-lifecycle-assessment-and-breakdown.md), Pantau mendeteksi usia masa pakai produktif hardware (*Productive Lifespan*) pada server fisik secara *agentless* melalui tanggal rilis DMI/SMBIOS motherboard (`/sys/class/dmi/id/bios_date`).

Namun, dalam skenario operasional riil:
1. **New Old Stock (NOS)**: Organisasi membeli server baru dari distributor yang merupakan stok lama pabrikan (misal motherboard diproduksi/ber-BIOS tahun 2018, tetapi unit baru dibeli dan dikomisioning pada tahun 2024/2026).
2. **Motherboard Tanpa Update Firmware**: Pabrikan tidak merilis pembaruan BIOS sejak rilis perdana, padahal unit baru beroperasi 1-2 tahun.

Pada kondisi ini, deteksi otomatis murni berbasis BIOS memicu *false positive*: server yang baru beroperasi 1 tahun langsung terkena penalti skor (-15 s.d. -25) dan rekomendasi peremajaan (*Hardware Refresh*). Di sisi lain, sistem tidak boleh sepenuhnya mengabaikan tanggal BIOS karena firmware lama tetap membawa implikasi keamanan mikroarsitektur (microcode vulnerability).

## Keputusan Arsitektur

1. **Atribut `Hardware Commission Date` pada Glosarium & Database**:
   - Menambahkan kolom `commission_date TEXT` (format ISO-8601 `YYYY-MM-DD`, opsional/nullable) pada tabel `hosts`.
   - Tercatat secara resmi dalam glosarium `CONTEXT.md` sebagai tanggal resmi dimulainya masa operasional penempatan server.

2. **Hierarki Precedensi Evaluasi Usia**:
   - **Tingkat 1 (Manual Override)**: Jika `commission_date` terisi dan valid, usia produktif hardware dihitung murni dari selisih waktu tanggal komisioning (`time.Since(commissionDate)`).
   - **Tingkat 2 (Fisik / Bare-Metal Otomatis)**: Jika `commission_date` kosong, mengacu pada tanggal rilis BIOS fisik (`/sys/class/dmi/id/bios_date`).
   - **Tingkat 3 (Virtual / Cloud Fallback)**: Jika virtual machine atau BIOS date tidak terbaca, mengacu pada OS deployment age (`/etc/machine-id`).

3. **Validasi Batas Tanggal (Pencegahan Anomali Logika)**:
   - **Batas Atas**: Tanggal komisioning tidak boleh berada di masa depan (`commission_date <= today`).
   - **Batas Bawah**: Tanggal komisioning tidak boleh lebih lampau dari tanggal rilis BIOS fisik motherboard (`commission_date >= bios_date`), karena secara fisik server mustahil beroperasi sebelum komponen motherboard-nya diproduksi.

4. **Transparansi Ganda pada Rincian Audit (`lifecycle_breakdown`)**:
   - Jika override aktif, teks diagnostik tetap mempertahankan jejak firmware asli:
     - `Within normal productive lifespan (0.3 yrs; Commissioned: 2026-01-15 • Motherboard BIOS: May 2017)`
   - Hal ini menjaga akurasi laporan operasional sekaligus mempertahankan peringatan teknis bahwa versi BIOS server bersangkutan sudah berumur dan dianjurkan untuk diperbarui.

5. **Kalkulasi Ulang Seketika (*Immediate Recalculation*)**:
   - Saat admin menyimpan atau mengubah `commission_date` pada dialog Host, sistem langsung menghitung ulang `LifecycleScore` dan `lifecycle_breakdown` menggunakan snapshot metrik terakhir di basis data tanpa perlu menunggu jadwal inspeksi SSH berikutnya.
