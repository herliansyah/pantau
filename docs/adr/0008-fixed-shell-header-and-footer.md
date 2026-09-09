# Fixed Shell Header dan Footer

Antarmuka web Pantau menerapkan struktur tata letak *fixed shell*: bilah atas (**Header**) dan bilah bawah (**Footer**) diposisikan tetap (`position: fixed; z-index: 40`), sementara area konten tengah (`.container`) bergulir secara independen dengan batas padding vertikal aman.

Pemisahan tanggung jawab informasi diterapkan secara tegas untuk mengeliminasi redundansi visual:
1. **Header (Operational Controls)**: Mengakomodasi identitas produk (`Pantau`), status peringatan langsung (Alerts badge), dan seluruh tombol aksi interaktif pengguna (`Transfers`, `Add Host`, `Settings`, `Logout`).
2. **Footer (Attribution & Legal)**: Mengakomodasi metadata open-source (lisensi MIT), atribusi pembuat (Herliansyah), dan tautan repositori GitHub publik (`https://github.com/herliansyah/pantau`).

Keputusan ini diambil untuk menghadirkan pengalaman antarmuka yang konsisten seperti aplikasi desktop native, memastikan akses cepat ke fungsi operasional server tanpa perlu menggulir kembali ke atas layar, sekaligus menyajikan kepatuhan lisensi dan tautan proyek tanpa membebani ruang kontrol utama.

Untuk memisahkan bidang pandang antara header/footer tetap dengan kartu-kartu server yang bergulir di bawahnya, diterapkan teknik *glassmorphism* (`rgba(30, 41, 59, 0.85)` dengan `backdrop-filter: blur(12px)`) serta bayangan jatuh bertingkat (*drop shadow* `box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4)` pada header dan bayangan inversi pada footer). Ini mencegah tabrakan visual warna datar saat kartu melintas di bawah bilah kontrol, memberikan ilusi kedalaman elevasi yang halus tanpa garis batas kaku.
