package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type MyClaims struct {
	UserId   uint   `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken 用 HS256 签发 token，返回 token 字符串和过期时间（Unix 秒）。
func GenerateToken(userID uint, username, secret string, issuer string, expireHours int) (string, int64, error) {
	now := time.Now()
	expiresAt := now.Add(time.Duration(expireHours) * time.Hour)

	claims := MyClaims{
		UserId:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", 0, err
	}
	return token, expiresAt.Unix(), nil
}

// ParseToken 校验签名、算法和有效期，通过后返回 claims。
// 过期时返回的 err 满足 errors.Is(err, jwt.ErrTokenExpired)，可据此区分「过期」和「非法」。
func ParseToken(tokenString, secret string) (*MyClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("签名算法不匹配")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*MyClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("无效的 Token")
}
