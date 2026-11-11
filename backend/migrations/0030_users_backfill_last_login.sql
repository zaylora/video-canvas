-- 注册即登录，但早先注册时没写 last_login_at，只注册、之后没再用密码登录过的用户在后台显示「从未登录」。
-- 用注册成功记录的时间回填。
UPDATE users u
SET last_login_at = l.first_at
FROM (
    SELECT user_id, MIN(created_at) AS first_at
    FROM user_login_logs
    WHERE kind = 'register' AND result = 'ok'
    GROUP BY user_id
) l
WHERE u.id = l.user_id AND u.last_login_at IS NULL;
