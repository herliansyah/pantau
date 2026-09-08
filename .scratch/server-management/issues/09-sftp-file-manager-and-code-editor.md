# 09 — SFTP File Manager & Lightweight Code/Text Editor

**What to build:** An in-browser file management interface using SFTP. Administrators can navigate directory trees on the target host, view file properties, upload and download files, change permissions (chmod), rename and delete files, and open text files (PHP, config files, bash scripts, .env) in an embedded CodeMirror editor to edit and save changes directly to the remote server.

**Blocked by:** 01 — Host Management & SSH Connection Verification.

**Status:** resolved

- [x] Backend SFTP client layer exposes directory listing, file reading, file writing, chmod, and deletion operations.
- [x] UI provides a responsive file explorer showing path breadcrumbs, file names, sizes, permissions, and modification dates.
- [x] Upload (multipart stream) and Download endpoints stream files reliably over SFTP.
- [x] Code/text editor modal powered by embedded CodeMirror with syntax highlighting for PHP, shell scripts, Nginx/conf, and plain text.
- [x] File save writes remote changes securely via SFTP with error handling.
- [x] Automated integration tests verify SFTP list, read, write, and permission change operations against a test SFTP server.
