package refreshtoken

import (
	"strings"
	"testing"
)

// TestGenerate 明文与哈希的形态契约。
//
// 契约：明文 43 字符 base64url（32 字节随机）、哈希 43 字符且**不等于**明文。
// 若两者相同则说明存储的就是明文，库被拖即可直接冒用。
func TestGenerate(t *testing.T) {
	plain, hash, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 43 {
		t.Fatalf("plain 长度 = %d, want 43", len(plain))
	}
	if len(hash) != 43 {
		t.Fatalf("hash 长度 = %d, want 43", len(hash))
	}
	if plain == hash {
		t.Fatal("hash 与 plain 相同——服务端会存到明文")
	}
	if !IsHashLike(Hash(plain)) {
		t.Fatal("Hash(plain) 不符合哈希形态")
	}
}

// TestGenerateUnique 两次生成必须不同（否则等于固定凭据）。
func TestGenerateUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		plain, hash, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[plain]; dup {
			t.Fatalf("第 %d 次生成了重复明文", i)
		}
		if _, dup := seen[hash]; dup {
			t.Fatalf("第 %d 次生成了重复哈希", i)
		}
		seen[plain] = struct{}{}
		seen[hash] = struct{}{}
	}
}

// TestHashDeterministic 同一明文恒得同一哈希（否则查库必然 miss）。
func TestHashDeterministic(t *testing.T) {
	plain := "some-refresh-token"
	if Hash(plain) != Hash(plain) {
		t.Fatal("Hash 不确定性——无法用于查库")
	}
	if Hash(plain) == Hash(plain+"x") {
		t.Fatal("不同明文得到相同哈希")
	}
}

// TestIsHashLike 拒绝明显非法的输入，避免拿超长串去查 Redis。
func TestIsHashLike(t *testing.T) {
	_, hash, _ := Generate()
	if !IsHashLike(hash) {
		t.Fatal("合法哈希应通过")
	}
	for _, bad := range []string{
		"",
		"short",
		strings.Repeat("a", 100),
		"has space here 123456789012345678901234567890",
	} {
		if IsHashLike(bad) {
			t.Fatalf("非法输入 %q 不应通过", bad)
		}
	}
}

// TestConstantTimeEqual 长度相同才可能相等。
func TestConstantTimeEqual(t *testing.T) {
	if ConstantTimeEqual("abc", "abc") != true {
		t.Fatal("相同应判等")
	}
	if ConstantTimeEqual("abc", "abd") != false {
		t.Fatal("不同应判不等")
	}
	if ConstantTimeEqual("abc", "abcd") != false {
		t.Fatal("长度不同应直接判不等")
	}
}
