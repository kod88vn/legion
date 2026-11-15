-- Test accounts (plain-text passwords; rAthena default use_MD5_passwords: no)
--   player / player   GM (group 99) for the human client
--   bot1   / bot1     normal account driven by OpenKore
INSERT INTO `login` (`account_id`,`userid`,`user_pass`,`sex`,`email`,`group_id`) VALUES
 (2000000,'player','player','M','a@a.com',99),
 (2000001,'bot1','bot1','M','a@a.com',0);

-- Pre-made characters so neither side needs the char-creation screen
INSERT INTO `char` (`char_id`,`account_id`,`char_num`,`name`,`class`,`base_level`,`job_level`,
  `str`,`agi`,`vit`,`int`,`dex`,`luk`,`max_hp`,`hp`,`max_sp`,`sp`,`status_point`,
  `hair`,`hair_color`,`last_map`,`last_x`,`last_y`,`save_map`,`save_x`,`save_y`)
VALUES
 (150000,2000000,0,'Admin',0,1,1,5,5,5,5,5,5,40,40,11,11,48,1,0,'prontera',156,180,'prontera',156,180),
 (150001,2000001,0,'AIBot',0,1,1,5,5,5,5,5,5,40,40,11,11,48,2,0,'prontera',150,180,'prontera',150,180);

-- starter weapon (Knife) for the bot
INSERT INTO `inventory` (`char_id`,`nameid`,`amount`,`equip`,`identify`) VALUES (150001,1201,1,2,1);
UPDATE `char` SET `weapon`=1201 WHERE char_id=150001;
