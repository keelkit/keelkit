package admin

import (
	"sync"
	"time"
)

// limiter — ограничение неудачных попыток входа: после max неудач за window ключ блокируется
// до конца окна (окно отсчитывается от первой неудачи). Ключ — email: перебор пароля одного
// пользователя ограничен, с какого бы числа адресов он ни шёл. Цена — злоумышленник может
// временно заблокировать вход пользователю; блокировка снимается сама по истечении окна.
//
// Состояние в памяти процесса: сайт — один бинарник, перезапуск сбрасывает счётчики.
type limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*attempts
}

type attempts struct {
	count int
	first time.Time
}

// maxLimiterEntries — порог, после которого из памяти удаляются истёкшие записи.
const maxLimiterEntries = 10_000

func newLimiter(maxFailures int, window time.Duration) *limiter {
	return &limiter{max: maxFailures, window: window, now: time.Now, entries: map[string]*attempts{}}
}

// blocked сообщает, заблокирован ли ключ, и сколько осталось ждать.
func (l *limiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.entries[key]
	if !ok {
		return 0, false
	}
	left := l.window - l.now().Sub(a.first)
	if left <= 0 {
		delete(l.entries, key)
		return 0, false
	}
	return left, a.count >= l.max
}

// fail учитывает неудачную попытку.
func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a, ok := l.entries[key]
	if !ok || now.Sub(a.first) >= l.window {
		if len(l.entries) >= maxLimiterEntries {
			l.prune(now)
		}
		l.entries[key] = &attempts{count: 1, first: now}
		return
	}
	a.count++
}

// reset сбрасывает счётчик после успешного входа.
func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *limiter) prune(now time.Time) {
	for k, a := range l.entries {
		if now.Sub(a.first) >= l.window {
			delete(l.entries, k)
		}
	}
}
