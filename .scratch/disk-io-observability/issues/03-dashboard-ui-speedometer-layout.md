# 03 — Representasi UI Dashboard & Komponen Speedometer

Type: prototype
Status: resolved
Blocked by: 02

## Question

Bagaimana tata letak penyajian live Disk I/O rate pada Host Card dan Host Detail Modal agar serasi berdampingan dengan Live Network Traffic (⬇️ / ⬆️) tanpa membuat card menjadi sesak atau memicu layout shift?

## Answer

### 1. Host Card (Dashboard Grid)
Menggunakan **Opsi A**: Menggabungkan Network Traffic dan Disk I/O dalam 1 baris stat-row ganda yang hemat ruang (`traffic-grid` 2x2 atau split pill).
```html
<div class="stat-row" style="flex-direction: column; gap: 4px;">
  <div style="display: flex; justify-content: space-between; font-size: 0.75rem; color: var(--text-muted);">
    <span>${t('traffic_net', 'Net')}</span>
    <span>${t('traffic_disk', 'Disk I/O')}</span>
  </div>
  <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 6px; font-size: 0.8rem; font-family: monospace;">
    <div style="display: flex; gap: 6px; background: rgba(56, 189, 248, 0.08); padding: 2px 6px; border-radius: 4px;">
      <span>⬇️ ${formatBytes(h.net_rx_speed_bps)}/s</span>
      <span>⬆️ ${formatBytes(h.net_tx_speed_bps)}/s</span>
    </div>
    <div style="display: flex; gap: 6px; background: rgba(168, 85, 247, 0.08); padding: 2px 6px; border-radius: 4px;">
      <span>📖 ${formatBytes(h.disk_read_speed_bps)}/s</span>
      <span>✍️ ${formatBytes(h.disk_write_speed_bps)}/s</span>
    </div>
  </div>
</div>
```

### 2. Host Detail Modal (Overview & Observability)
Menambahkan 1 Card Metrik khusus di grid ringkasan:
- **Judul**: `Disk I/O Rate` / `Laju Disk I/O`
- **Angka Utama**: `📖 ${formatBytes(h.disk_read_speed_bps)}/s | ✍️ ${formatBytes(h.disk_write_speed_bps)}/s` (aksen warna ungu `#c084fc`)
- **Sub-teks**: `Total Read: ${formatBytes(h.disk_read_bytes)} | Write: ${formatBytes(h.disk_write_bytes)}`
- **Responsive**: Masuk ke grid card yang sudah ada (`grid-template-columns: repeat(auto-fit, minmax(220px, 1fr))`) sehingga otomatis menyesuaikan lebar layar tanpa mengubah struktur layout.

