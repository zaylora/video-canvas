const tokenKey = 'video_design_token'
const expireAtKey = 'video_design_token_expire_at'

/** 读取登录 token，未登录时返回 null */
export const getToken = () => {
  const expireAt = Number(localStorage.getItem(expireAtKey))
  if (expireAt && Date.now() >= expireAt * 1000) {
    removeToken()
    return null
  }
  return localStorage.getItem(tokenKey)
}

/** 写入登录 token */
export const setToken = (token: string, expireAt: number) => {
  localStorage.setItem(tokenKey, token)
  localStorage.setItem(expireAtKey, String(expireAt))
}

/** 清除登录 token */
export const removeToken = () => {
  localStorage.removeItem(tokenKey)
  localStorage.removeItem(expireAtKey)
}
