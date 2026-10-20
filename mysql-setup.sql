-- Videoscope 数据库初始化
-- 用法（在 WSL 中执行，需要 root 权限）:
--   sudo mysql < /mnt/c/Users/lyz/videoscope/mysql-setup.sql
--
-- 只建库和授权，表结构由程序启动时的 AutoMigrate 创建。

CREATE DATABASE IF NOT EXISTS videoscope
    CHARACTER SET utf8mb4
    COLLATE utf8mb4_unicode_ci;

CREATE USER IF NOT EXISTS 'goapp'@'localhost' IDENTIFIED BY 'goapp123';

GRANT ALL PRIVILEGES ON videoscope.* TO 'goapp'@'localhost';

FLUSH PRIVILEGES;
