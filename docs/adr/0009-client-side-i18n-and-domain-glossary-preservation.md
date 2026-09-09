# Client-Side i18n dan Pelestarian Glosarium Domain

Pantau mengadopsi sistem lokalisasi dwibahasa (Bahasa Inggris sebagai default dan Bahasa Indonesia) secara langsung pada sisi klien (*client-side*) dalam berkas statis `index.html` tanpa dependensi pustaka pihak ketiga maupun overhead template rendering pada sisi server Go.

Pilihan bahasa pengguna disimpan pada `localStorage` browser (`pantau_lang`), dan dialihkan secara dinamis melalui tombol alih bahasa (`🌐 EN | ID`) pada bilah atas (Header) dan tampilan login. Komponen teks antarmuka menggunakan atribut deklaratif `data-i18n` untuk elemen statis dan fungsi pembantu `t(key, fallback)` untuk teks dinamis, modal dialog, serta pesan notifikasi interaktif.

Terminologi teknis domain inti yang telah dibakukan dalam `CONTEXT.md` (*Host*, *Desired State*, *Actual State*, *Drift*, *Root Cause Excerpt*, *Lifecycle Score*, *System Snapshot*, *Cross-Host Transfer*) tetap dipertahankan secara kanonikal dalam kedua bahasa. Langkah ini memastikan ketepatan semantik dan mencegah kerancuan operasional bagi administrator sistem Linux (misalnya menghindari penerjemahan janggal seperti "Inang" atau "Deviasi Keadaan").

Keputusan ini diambil dengan menolak ketergantungan pada pustaka i18n eksternal (seperti i18next via CDN) atau server-side rendering template Go. Pendekatan kamus mandiri zero-dependency menjaga arsitektur *single binary* Pantau tetap ringan, instan dimuat tanpa koneksi internet tambahan, dan mempertahankan footprint memori serta ukuran distribusi biner yang minimal.
