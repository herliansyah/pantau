# Piped Streaming Cross-Host Transfer

Pemindahan file atau folder antar host (Cross-Host Transfer) dilakukan menggunakan model streaming pipa (*piped tar streaming*) yang direlay melalui memori Pantau, tanpa membuat file arsip sementara pada disk server master.

Keputusan ini diambil agar transfer antar server dapat bekerja secara andal meskipun kedua server target berada pada jaringan terisolasi (beda VPC, beda cloud provider, atau terhalang firewall/NAT) tanpa perlu mengekspos kredensial SSH antar host. Dengan mengalirkan data melalui buffer memori konstan (1MB), Pantau mampu mentransfer data berukuran ratusan gigabyte (hingga 500GB) dengan penggunaan RAM yang sangat rendah (< 20MB) dan nol penggunaan harddisk pada server master.
