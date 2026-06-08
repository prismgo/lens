//go:build windows

package lens

import (
	"golang.org/x/sys/windows/registry"
)

// windowsUserPATHStore 记录读取到的注册表类型，避免 REG_EXPAND_SZ 被写回成 REG_SZ。
type windowsUserPATHStore struct {
	valueType uint32
}

func (store *windowsUserPATHStore) ReadUserPATH() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("Path")
	if err == registry.ErrNotExist {
		store.valueType = registry.SZ
		return "", nil
	}
	store.valueType = valueType
	return value, err
}

func (store *windowsUserPATHStore) WriteUserPATH(value string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if store.valueType == registry.EXPAND_SZ {
		return key.SetExpandStringValue("Path", value)
	}
	return key.SetStringValue("Path", value)
}

// WriteUserPATH 将 binDir 追加到当前 Windows 用户的 HKCU Environment Path。
func WriteUserPATH(binDir string) ([]string, error) {
	store := &windowsUserPATHStore{}
	wrote, err := appendUserPATH(store, binDir, ";", true)
	if err != nil {
		return nil, err
	}
	if !wrote {
		return nil, nil
	}
	return []string{`HKCU\Environment\Path`}, nil
}
