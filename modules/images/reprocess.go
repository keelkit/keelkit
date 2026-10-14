package images

import (
	"context"
	"errors"
	"fmt"
)

// ReprocessStats — итог Reprocess.
type ReprocessStats struct {
	Processed int // изображения, получившие варианты текущего профиля
	Failed    int // изображения, которые не удалось обработать (причины — в ошибке Reprocess)
}

// Reprocess строит варианты текущего профиля (ProfileVersion) для изображений с версией профиля ниже below.
// below больше ProfileVersion равносилен ProfileVersion: выше текущей версии подняться нельзя, поэтому
// повторный запуск с тем же below ничего не делает.
//
// Изображения обрабатываются по одному из оригиналов в хранилище. Для каждого: новые варианты
// записываются в «<id>/v<профиль>/…», затем в одной транзакции меняются строки вариантов и версия,
// затем удаляются файлы старых вариантов. Прерывание (отмена ctx, падение процесса) в любой момент
// оставляет изображение либо со старыми вариантами, либо с новыми; повторный запуск доделывает работу.
//
// Ошибка отдельного изображения (например, нет оригинала) не останавливает остальные: оно считается
// в Failed, а причины собираются в возвращаемую ошибку. Отмена ctx останавливает работу сразу.
func (s *Service) Reprocess(ctx context.Context, below int) (ReprocessStats, error) {
	var stats ReprocessStats
	target := min(below, s.profile)
	if err := s.emptyTrash(ctx); err != nil {
		return stats, err
	}

	var errs []error
	after := "" // постранично по id: упавшие изображения не выбираются снова в этом же запуске
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT id, profile_version, original_key FROM images_files
			WHERE profile_version < ? AND id > ? ORDER BY id LIMIT 100`, target, after)
		if err != nil {
			return stats, fmt.Errorf("images: reprocess: %w", err)
		}
		type candidate struct {
			id, origKey string
			version     int
		}
		var batch []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.version, &c.origKey); err != nil {
				_ = rows.Close()
				return stats, fmt.Errorf("images: reprocess: %w", err)
			}
			batch = append(batch, c)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return stats, fmt.Errorf("images: reprocess: %w", err)
		}
		if len(batch) == 0 {
			return stats, errors.Join(errs...)
		}

		for _, c := range batch {
			after = c.id
			err := s.reprocessOne(ctx, c.id, c.version, c.origKey)
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			if err != nil {
				stats.Failed++
				errs = append(errs, fmt.Errorf("image %s: %w", c.id, err))
				continue
			}
			stats.Processed++
		}
	}
}

func (s *Service) reprocessOne(ctx context.Context, id string, version int, origKey string) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	res, err := s.processStored(ctx, origKey)
	s.release()
	if err != nil {
		return err
	}

	vs, err := s.putVariants(ctx, id, res.Variants)
	if err == nil {
		err = s.commitReprocess(ctx, id, version, vs, res.Placeholder)
	}
	if err != nil {
		// Записанные файлы не трогаем: если это ключи живых вариантов (та же версия профиля),
		// удаление их сломало бы; повторный запуск всё равно перезапишет их.
		return err
	}
	_ = s.emptyTrash(ctx) // остатки удалит следующий вызов
	return nil
}

func (s *Service) processStored(ctx context.Context, key string) (*Result, error) {
	rc, err := s.st.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("original: %w", err)
	}
	defer func() { _ = rc.Close() }()
	return Process(ctx, rc)
}

// commitReprocess подменяет варианты изображения на vs и заглушку на placeholder, если с момента выборки его не удалили и не
// обработали заново (версия всё ещё version). Иначе новые файлы, на которые никто не ссылается, — в корзину.
func (s *Service) commitReprocess(ctx context.Context, id string, version int, vs []StoredVariant, placeholder string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("images: reprocess: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	newKeys := make([]string, len(vs))
	for i, v := range vs {
		newKeys[i] = v.Path
	}
	r, err := tx.ExecContext(ctx, `UPDATE images_files SET profile_version = ?, placeholder = ?
		WHERE id = ? AND profile_version = ?`, s.profile, placeholder, id, version)
	if err != nil {
		return fmt.Errorf("images: reprocess: %w", err)
	}
	if n, err := r.RowsAffected(); err != nil {
		return fmt.Errorf("images: reprocess: %w", err)
	} else if n == 0 {
		if err := trash(ctx, tx, newKeys); err != nil {
			return err
		}
		return tx.Commit()
	}

	oldKeys, err := queryStrings(ctx, tx, `DELETE FROM images_variants WHERE image_id = ? RETURNING key`, id)
	if err != nil {
		return fmt.Errorf("images: reprocess: %w", err)
	}
	if err := insertVariants(ctx, tx, id, vs); err != nil {
		return err
	}
	if err := trash(ctx, tx, oldKeys); err != nil { // trash пропустит ключи, совпавшие с новыми
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("images: reprocess: %w", err)
	}
	return nil
}
