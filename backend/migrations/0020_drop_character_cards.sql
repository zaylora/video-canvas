-- 角色卡不再是后端全局实体：画布里的角色卡是普通节点，角色数据内嵌在画布 payload 的
-- data.character 里，只属于当前画布。旧角色卡数据不迁移、直接清掉（设计 画布角色卡重设计 §11.1-5）。
-- DROP TABLE 会连带删除表上的主键与全部索引（idx_character_cards_user_updated、idx_character_cards_drama_id）。
-- 代码已不再读写这张表：/characters 接口与 model.CharacterCard 在同一版本下线，
-- 所以不必再拆成"先停用、后删除"两个版本；退回上一版镜像时旧版代码会因表缺失而无法读角色卡，
-- 这是「清掉旧数据」决定的预期后果。
DROP TABLE IF EXISTS character_cards;
