-- MCP 权限从三档粗粒度拆成六档，并把已有项目的能力展开到新档位。
--
-- 旧值映射（保持原有能力不变，只是拆开）：
--   canvas:read    → canvas:browse（新，原本一起吃掉的浏览类）
--                    + canvas:read（收窄为「读画布结构」）
--                    + canvas:media（原本一起吃掉的看图看视频抓帧）
--   canvas:write   → 原样保留（仍是「改画布」）；canvas:arrange 另拆出 canvas:layout
--   canvas:compose → 原样保留
--
-- 另外任何非空权限都补 canvas:browse：所有画布类工具都要先知道画布 code。
UPDATE mcp_projects
SET scopes = (
        SELECT COALESCE(to_jsonb(array_agg(DISTINCT v)), '[]'::jsonb)
        FROM jsonb_array_elements_text(
            scopes
            || '["canvas:browse"]'::jsonb
            || CASE WHEN scopes @> '["canvas:read"]'::jsonb THEN '["canvas:media"]'::jsonb ELSE '[]'::jsonb END
        ) AS v
    )
WHERE jsonb_array_length(scopes) > 0;

COMMENT ON COLUMN mcp_projects.scopes IS
    '权限档位：canvas:browse / read / media / write / layout / compose；后四档隐含 canvas:read，全档隐含 canvas:browse';
