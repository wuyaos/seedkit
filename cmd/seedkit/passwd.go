package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wuyaos/seedkit/internal/config"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

func passwd(dataDir string, in *os.File, out io.Writer) error {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("需要交互终端；Docker 请使用 docker exec -it seedkit seedkit passwd")
	}
	return resetPassword(dataDir, func(prompt string) ([]byte, error) {
		fmt.Fprint(out, prompt)
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		return password, err
	}, out)
}

// Update only the stored hash: do not persist environment overrides or erase
// unrelated YAML keys. The server reloads credentials and clears sessions on restart.
func resetPassword(dataDir string, readPassword func(string) ([]byte, error), out io.Writer) error {
	if _, ok := os.LookupEnv("SEEDKIT_AUTH_PASSWORD_HASH"); ok {
		return errors.New("请先移除 SEEDKIT_AUTH_PASSWORD_HASH 环境变量，否则重启后仍会覆盖配置中的密码")
	}
	path := config.Path(dataDir)
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置 %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return fmt.Errorf("解析配置: %w", err)
	}
	if len(doc.Content) == 0 {
		return errors.New("尚未初始化管理员，请先在网页创建账号")
	}
	auth := yamlValue(doc.Content[0], "auth")
	user, hash := yamlValue(auth, "username"), yamlValue(auth, "password_hash")
	if user == nil || user.Value == "" || hash == nil || hash.Value == "" {
		return errors.New("尚未初始化管理员，请先在网页创建账号")
	}
	fmt.Fprintf(out, "重置管理员 %s 的密码（配置：%s）\n", user.Value, path)
	password, err := readPassword("新密码（6–72 字节）：")
	if err != nil {
		return err
	}
	defer clear(password)
	if len(password) < 6 || len(password) > 72 {
		return errors.New("密码长度必须为 6–72 字节")
	}
	confirmation, err := readPassword("再次输入新密码：")
	if err != nil {
		return err
	}
	defer clear(confirmation)
	if !bytes.Equal(password, confirmation) {
		return errors.New("两次密码不一致，配置未修改")
	}
	encoded, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	hash.Tag, hash.Value = "!!str", string(encoded)
	updated, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	// Use a unique temporary file in the same directory for atomic replacement.
	tmp, err := os.CreateTemp(dataDir, ".passwd-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(updated); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	fmt.Fprintln(out, "密码已更新。请重启 seedkit 服务后使用新密码登录；旧会话将在重启后失效。Docker：docker restart seedkit")
	return nil
}

func yamlValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
