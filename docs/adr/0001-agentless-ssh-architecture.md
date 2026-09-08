# Agentless SSH Architecture

Pemantauan dan manajemen host dilakukan murni melalui koneksi SSH standar (pull-based inspection dan remote execution), bukan menggunakan daemon agen yang diinstal pada masing-masing server target.

Keputusan ini diambil untuk menghindari overhead instalasi, pemeliharaan, dan pembaruan agen pada server-server target, sekaligus memanfaatkan akses SSH yang sudah menjadi prasyarat untuk web terminal dan manajemen file (SFTP). Konsekuensinya, server Pantau mengelola concurrency koneksi SSH dan pembacaan metrics berbasis shell commands standar Linux.
