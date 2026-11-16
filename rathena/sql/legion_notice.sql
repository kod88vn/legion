-- Server-wide notices read by the MarketCrier floating NPC (rathena/npc-custom/legion_crier.txt).
-- Written only by tools/system/crier.py (one INSERT per notice). The NPC announces the oldest row with sent = 0,
-- then sets sent = 1. Load once into ro-db (ragnarok database); CREATE TABLE IF NOT EXISTS keeps it idempotent.
CREATE TABLE IF NOT EXISTS `legion_notice` (
  `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
  `text` VARCHAR(120) NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `sent` TINYINT(1) NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `legion_notice_pending` (`sent`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
