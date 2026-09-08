# Golang Single-Binary dan Embedded SQLite

Pantau dibangun menggunakan bahasa Go yang dikompilasi menjadi satu file binary mandiri (single binary), dengan SQLite (WAL mode) sebagai basis data dan aset web antarmuka yang di-embed langsung ke dalam binary.

Keputusan ini diambil untuk meminimalkan jejak memori (< 30MB RAM), menghilangkan kebutuhan dependensi runtime eksternal (zero-ops), serta memudahkan deployment instan pada VPS atau container Docker tunggal. Concurrency inspeksi SSH ditangani secara efisien melalui goroutine dan pustaka standar/resmi `golang.org/x/crypto/ssh` serta `github.com/pkg/sftp`.
