package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const RuntimeDir = "/tmp/fastlane-update"

type State struct {
	Status    string     `json:"status"`
	Message   string     `json:"message"`
	Current   string     `json:"current_version"`
	Candidate *Candidate `json:"candidate,omitempty"`
	CheckedAt time.Time  `json:"checked_at,omitempty"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	Token     string     `json:"token,omitempty"`
	PID       int        `json:"pid,omitempty"`
}

func (s State) Busy() bool { return s.Status == "checking" || s.Status == "installing" }

type Manager struct {
	Dir, Current, Arch string
	Client             *http.Client
	// Spawn starts a detached worker and returns its PID. It must not wait for it.
	Spawn   func(operation, token string) (int, error)
	Install func(context.Context, Candidate, []byte) error
}

func (m *Manager) lock() (*os.File, error) {
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(m.Dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("update directory must be private")
	}
	f, err := os.OpenFile(filepath.Join(m.Dir, "lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("Обновление уже выполняется. Дождитесь завершения.")
	}
	return f, nil
}

func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }

func (m *Manager) read() (State, error) {
	s := State{Status: "idle", Current: m.Current, Message: "Проверьте, доступна ли новая версия Fast Lane."}
	b, err := os.ReadFile(filepath.Join(m.Dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	s.Current = m.Current
	return s, nil
}

func (m *Manager) write(s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, "state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.Dir, "state.json"))
}

func alive(s State) bool {
	if !s.Busy() {
		return false
	}
	if s.PID == 0 {
		return time.Since(s.StartedAt) < 15*time.Second
	}
	return syscall.Kill(s.PID, 0) == nil && time.Since(s.StartedAt) < 20*time.Minute
}

// Status never performs network IO and does not wait for the installer lock.
func (m *Manager) Status() (State, error) {
	s, err := m.read()
	if err != nil {
		return s, err
	}
	if s.Busy() && !alive(s) {
		s.Status = "interrupted"
		s.Message = "Обновление было прервано. Проверьте версию и повторите проверку."
	}
	return s, nil
}

func (m *Manager) Start(operation string, releaseID int64) (State, error) {
	if operation != "check" && operation != "install" {
		return State{}, errors.New("invalid update operation")
	}
	f, err := m.lock()
	if err != nil {
		return State{}, err
	}
	defer unlock(f)
	s, err := m.read()
	if err != nil {
		return s, err
	}
	if alive(s) {
		return s, nil
	}
	if operation == "install" {
		if s.Status != "available" || s.Candidate == nil || releaseID <= 0 || s.Candidate.ID != releaseID || time.Since(s.CheckedAt) > 30*time.Minute {
			return s, errors.New("Сначала заново проверьте обновления и подтвердите найденную версию.")
		}
		comparison, err := Compare(s.Candidate.Version, m.Current)
		if err != nil || comparison <= 0 {
			return s, errors.New("Установка этой версии больше не требуется.")
		}
		s.Status = "installing"
		s.Message = "Загружаю и устанавливаю обновление. Не выключайте роутер."
	} else {
		s = State{Status: "checking", Message: "Проверяю релизы на GitHub…", Current: m.Current}
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return s, err
	}
	s.Token = hex.EncodeToString(token[:])
	s.StartedAt = time.Now().UTC()
	s.PID = 0
	if err = m.write(s); err != nil {
		return s, err
	}
	pid, err := m.Spawn(operation, s.Token)
	if err != nil {
		s.Status = "error"
		s.Message = "Не удалось запустить фоновое обновление."
		_ = m.write(s)
		return s, err
	}
	s.PID = pid
	return s, m.write(s)
}

// Run owns the job independently of the browser, RPC request and Fast Lane daemon.
func (m *Manager) Run(ctx context.Context, operation, token string) error {
	// Start briefly owns this lock while saving the worker PID.
	var f *os.File
	var err error
	for i := 0; i < 50; i++ {
		f, err = m.lock()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err != nil {
		return err
	}
	defer unlock(f)
	s, err := m.read()
	if err != nil {
		return err
	}
	if s.Token != token || !s.Busy() || (operation == "check" && s.Status != "checking") || (operation == "install" && s.Status != "installing") {
		return errors.New("stale update job")
	}
	if operation != "check" && operation != "install" {
		return errors.New("invalid update operation")
	}
	s.PID = os.Getpid()
	if err = m.write(s); err != nil {
		return err
	}
	if operation == "check" {
		err = m.check(ctx, &s)
	} else {
		err = m.install(ctx, &s)
	}
	if err != nil {
		s.Status = "error"
		s.Message = "Обновление не выполнено: " + err.Error()
		var channel *ChannelError
		if errors.As(err, &channel) {
			s.Status = channel.Code
			s.Message = channel.Message
		}
	}
	s.Token = ""
	s.PID = 0
	return m.write(s)
}

func (m *Manager) check(ctx context.Context, s *State) error {
	release, err := Fetch(ctx, m.Client, "")
	if err != nil {
		return err
	}
	candidate, err := Select(release, m.Arch)
	if err != nil {
		return err
	}
	comparison, err := Compare(candidate.Version, m.Current)
	if err != nil {
		return err
	}
	s.Candidate = &candidate
	s.CheckedAt = time.Now().UTC()
	s.Status = "available"
	s.Message = "Доступна Fast Lane " + candidate.Version + "."
	if comparison == 0 {
		s.Status = "current"
		s.Message = "Установлена актуальная версия."
	}
	if comparison < 0 {
		s.Status = "newer"
		s.Message = "Установленная версия новее опубликованного релиза. Понижение отключено."
	}
	return nil
}

func (m *Manager) install(ctx context.Context, s *State) error {
	if s.Candidate == nil {
		return errors.New("no checked release")
	}
	checked := *s.Candidate
	release, err := Fetch(ctx, m.Client, checked.Tag)
	if err != nil {
		return err
	}
	candidate, err := Select(release, m.Arch)
	if err != nil {
		return err
	}
	if candidate != checked {
		return errors.New("релиз изменился после проверки; проверьте обновления заново")
	}
	script, err := Download(ctx, m.Client, candidate.Installer, 1024*1024)
	if err != nil {
		return err
	}
	if m.Install == nil {
		return errors.New("installer unavailable")
	}
	if err = m.Install(ctx, candidate, script); err != nil {
		return fmt.Errorf("установка не завершена: %w", err)
	}
	s.Status = "updated"
	s.Message = "Fast Lane " + candidate.Version + " установлена. Обновите страницу админки."
	return nil
}
