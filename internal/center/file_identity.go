package center

import (
	"errors"
	"os"
	"path/filepath"
)

// fileIdentity 是替换前看到的普通文件身份。新文件不能把目录的执行位抄过来。
type fileIdentity struct {
	mode     os.FileMode
	uid      int
	gid      int
	hasOwner bool
}

// 由各平台文件赋值。测试可以临时替换，用来确认写入时确实套用了原来的 uid/gid。
var (
	ownerOf   func(os.FileInfo) (int, int, bool)
	chownFile func(path string, uid, gid int) error
)

// targetFileIdentity 在替换前取值。已有文件沿用它的所有者和权限；没有文件时跟目录的所有者，模式固定 0600。
func targetFileIdentity(path string) fileIdentity {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		uid, gid, ok := ownerOf(info)
		return fileIdentity{mode: info.Mode().Perm(), uid: uid, gid: gid, hasOwner: ok}
	}
	id := fileIdentity{mode: 0o600}
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && info.IsDir() {
		uid, gid, ok := ownerOf(info)
		id.uid, id.gid, id.hasOwner = uid, gid, ok
	}
	return id
}

// repairConfigOwnership 只改所有者和组，不改内容和权限位。
func repairConfigOwnership(configPath string) error {
	dir := filepath.Dir(configPath)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("配置目录无效")
	}
	uid, gid, ok := ownerOf(info)
	if !ok || chownFile == nil {
		return errors.New("无法读取配置目录所有者")
	}
	updated := 0
	for _, path := range []string{configPath, filepath.Join(dir, "setup-token")} {
		item, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if !item.Mode().IsRegular() {
			return errors.New("refusing to replace a non-regular file")
		}
		if err = chownFile(path, uid, gid); err != nil {
			return err
		}
		updated++
	}
	if updated == 0 {
		return errors.New("没有可校正的文件")
	}
	return nil
}

func applyIdentity(path string, id fileIdentity) error {
	if err := os.Chmod(path, id.mode.Perm()); err != nil {
		return err
	}
	if !id.hasOwner {
		return nil
	}
	if chownFile == nil {
		return errors.New("无法设置文件所有者")
	}
	return chownFile(path, id.uid, id.gid)
}

// writeReplacing 先写同目录临时文件并套上目标身份，再改名。chown 失败时不替换原文件。
func writeReplacing(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular file")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if info, err := os.Lstat(temp); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular file")
	}
	id := targetFileIdentity(path)
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, id.mode.Perm())
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(temp)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(temp)
		return closeErr
	}
	if err = applyIdentity(temp, id); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}
