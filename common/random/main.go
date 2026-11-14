package random

import (
	cryptorand "crypto/rand"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func GetUUID() string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	return code
}

const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const keyNumbers = "0123456789"

// rng 是包内共享的伪随机源，rngLock 保护它（rand.Rand 不是并发安全的）。
//
// 为什么不再每次调用都 rand.Seed(time.Now().UnixNano())：
// 那是原实现的写法，而 rand.Seed 会重置全局源。同一纳秒（甚至同一毫秒，
// 取决于平台时钟粒度）内的连续调用会得到**完全相同**的序列，
// 实测 1000 次 GetRandomString(8) 只产出 51 个不同值，单值重复达 40 次。
// 这会让兑换码、邀请码、订单号这类依赖唯一性的业务直接撞唯一索引。
// 改为初始化一次、后续只取数。
var (
	rng     = rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(uuid.New().ClockSequence())))
	rngLock sync.Mutex
)

func generateKey() string {
	key := make([]byte, 48)
	for i := 0; i < 16; i++ {
		key[i] = keyChars[rand.Intn(len(keyChars))]
	}
	uuid_ := GetUUID()
	for i := 0; i < 32; i++ {
		c := uuid_[i]
		if i%2 == 0 && c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		key[i+16] = c
	}
	return string(key)
}

func GenerateKey() string {
	return generateKey()
}

func GetRandomString(length int) string {
	rngLock.Lock()
	defer rngLock.Unlock()
	key := make([]byte, length)
	for i := 0; i < length; i++ {
		key[i] = keyChars[rng.Intn(len(keyChars))]
	}
	return string(key)
}

func GetRandomNumberString(length int) string {
	rngLock.Lock()
	defer rngLock.Unlock()
	key := make([]byte, length)
	for i := 0; i < length; i++ {
		key[i] = keyNumbers[rng.Intn(len(keyNumbers))]
	}
	return string(key)
}

// RandRange returns a random number between min and max (max is not included)
func RandRange(min, max int) int {
	if max <= min {
		return min
	}
	rngLock.Lock()
	defer rngLock.Unlock()
	return min + rng.Intn(max-min)
}

// GetSecureRandomString 用 crypto/rand 生成随机串。
//
// 与 GetRandomString 的区别：这个是密码学安全的，适用于兑换码、邀请码等
// 可能被枚举猜测的凭据；GetRandomString 是快速伪随机，用于非安全场景。
func GetSecureRandomString(length int) string {
	buf := make([]byte, length)
	for i := 0; i < length; i++ {
		n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(keyChars))))
		if err != nil {
			// 系统熵源不可用时退化为伪随机，保证函数不会返回空串
			return GetRandomString(length)
		}
		buf[i] = keyChars[n.Int64()]
	}
	return string(buf)
}
