# 03 — Generator Narasi Kesimpulan Keputusan & Ekspor Markdown

Type: prototype
Status: resolved
Blocked by: 01, 02

## Question

Bagaimana formulasi algoritma generator narasi keputusan otomatis berbasis deteksi deviasi/keunggulan metrik paling signifikan (misal: "Host A direkomendasikan untuk workload berat karena keunggulan CPU & Free RAM, sementara Host B lebih unggul pada latensi jaringan"), serta bagaimana template format teks Markdown ringkas dan fungsi salin satu-klik (*Copy to Clipboard*) dirancang agar siap pakai untuk dokumentasi laporan atau bahan rapat pengambil keputusan?

## Answer

### 1. Algoritma Generator Narasi Keputusan (Decision Narrative Generator)

Fungsi JavaScript murni (`generateDecisionNarrative(hosts, scores)`) berjalan otomatis saat komparasi selesai dihitung, dengan alur 4 tahap:

1. **Deteksi Juara & Peringkat (Overall Standing)**:
   - Mengurutkan host berdasarkan total perolehan poin (*Best-in-Class*).
   - Menetapkan predikat: `🏆 Overall Winner`, `🥈 Runner-up`, atau `🤝 Co-leaders` bila terjadi skor seri di peringkat teratas.
2. **Ekstraksi Kekuatan Spesifik (Distinct Strengths)**:
   - Mengidentifikasi kategori yang dimenangkan oleh masing-masing host:
     - **Compute Winner**: Mengangkat keunggulan core (`cpu_cores`) dan ketersediaan memori bebas.
     - **Storage Winner**: Mengangkat keunggulan total kapasitas disk dan sisa ruang bebas.
     - **Network Winner**: Mengangkat keunggulan responsivitas latensi internet (`internet_latency_ms`).
     - **Hygiene Winner**: Mengangkat keunggulan stabilitas dan kebaruan sistem (`lifecycle_score`, zero drift).
3. **Deteksi Titik Kritis & Peringatan Risiko (Risk Flags)**:
   - Mendeteksi host dengan status `degraded`/`down`, sisa RAM < 15%, sisa Disk < 15%, atau CPU load/core > 1.0.
   - Menambahkan catatan peringatan langsung: misal *"⚠️ Host X mendekati saturasi penyimpanan (sisa disk < 15%)"*.
4. **Sintesis Kalimat Keputusan (Executive Takeaway)**:
   - Menghasilkan 2–3 kalimat lugas bahasa Indonesia yang langsung merekomendasikan alokasi peran:
     - *"Untuk penempatan workload komputasi berat, prioritaskan **[Host A]**..."*
     - *"Untuk service berorientasi latensi / web traffic, **[Host B]** paling efisien..."*
     - *"Hindari penambahan tugas pada **[Host C]** sebelum dilakukan pemeliharaan/upgrade..."*

### 2. Format Template Ekspor Markdown

Struktur output teks yang disalin ke clipboard:

```markdown
# ⚖️ Pantau — Laporan Komparasi Host
_Diekspor pada: <TIMESTAMP>_

### 🏆 Papan Skor Evaluasi
| Host | Status | Compute | Storage | Network | Hygiene | Total Poin | Predikat |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| **prod-db-primary** | Healthy | 2 | 2 | 1 | 0 | **5 ⭐** | 🏆 Overall Winner |
| **prod-app-01** | Healthy | 1 | 1 | 1 | 2 | **5 ⭐** | 🥈 Runner-up |
| **staging-worker** | Degraded | 0 | 0 | 0 | 0 | **0** | ⚠️ At Risk |

### 💡 Rekomendasi Keputusan
- **Beban Komputasi Utama**: Prioritaskan **prod-db-primary** karena keunggulan 8 Cores dan penyimpanan 500 GB.
- **Efisiensi & Stabilitas**: **prod-app-01** memiliki ruang memori bebas terbaik (Free RAM 62.5%) dan Lifecycle Score 94.
- **Peringatan Pemeliharaan**: **staging-worker** berstatus degraded dengan saturasi memori tinggi, tidak direkomendasikan untuk workload baru.

---
_Pantau Server Observability & Infrastructure Management_
```

### 3. Fungsi Salin Satu-Klik (`copyComparisonMarkdown`)

- Memanggil `navigator.clipboard.writeText(markdownContent)` native browser.
- Menampilkan feedback visual non-blocking via `showToast('Rangkuman perbandingan berhasil disalin ke clipboard!', 'success')` bawaan Pantau.
- Dilengkapi fallback textarea jika clipboard permission dibatasi oleh browser.
