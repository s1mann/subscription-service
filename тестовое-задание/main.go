package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

type Sub struct {
	ID    string  `json:"id"`
	Name  string  `json:"service_name"`
	Price int     `json:"price"`
	User  string  `json:"user_id"`
	Start string  `json:"start_date"`
	End   *string `json:"end_date,omitempty"`
}

type SubReq struct {
	Name  string  `json:"service_name"`
	Price int     `json:"price"`
	User  string  `json:"user_id"`
	Start string  `json:"start_date"`
	End   *string `json:"end_date,omitempty"`
}

type Storage interface {
	Create(ctx context.Context, s *Sub) error
	Get(ctx context.Context, id string) (*Sub, error)
	Update(ctx context.Context, s *Sub) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]Sub, error)
	Sum(ctx context.Context, from, to, user, name string) (int, error)
}

type Postgres struct {
	db *sql.DB
}

func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

func (p *Postgres) Create(ctx context.Context, s *Sub) error {
	log.Printf("сохраняю подписку %s для юзера %s", s.Name, s.User)
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO subscriptions (id, name, price, user_id, start_date, end_date) VALUES ($1,$2,$3,$4,$5,$6)`,
		s.ID, s.Name, s.Price, s.User, s.Start, s.End)
	if err != nil {
		log.Printf("ошибка при сохранении: %v", err)
	}
	return err
}

func (p *Postgres) Get(ctx context.Context, id string) (*Sub, error) {
	log.Printf("ищу подписку %s", id)
	var s Sub
	var end sql.NullString
	err := p.db.QueryRowContext(ctx,
		`SELECT id, name, price, user_id, start_date, end_date FROM subscriptions WHERE id=$1`, id).
		Scan(&s.ID, &s.Name, &s.Price, &s.User, &s.Start, &end)
	if err == sql.ErrNoRows {
		log.Printf("подписка %s не найдена", id)
		return nil, nil
	}
	if err != nil {
		log.Printf("ошибка при поиске %s: %v", id, err)
		return nil, err
	}
	if end.Valid {
		s.End = &end.String
	}
	log.Printf("нашел подписку %s", id)
	return &s, nil
}

func (p *Postgres) Update(ctx context.Context, s *Sub) error {
	log.Printf("обновляю подписку %s", s.ID)
	res, err := p.db.ExecContext(ctx,
		`UPDATE subscriptions SET name=$1, price=$2, user_id=$3, start_date=$4, end_date=$5 WHERE id=$6`,
		s.Name, s.Price, s.User, s.Start, s.End, s.ID)
	if err != nil {
		log.Printf("ошибка при обновлении %s: %v", s.ID, err)
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		log.Printf("подписка %s не найдена при обновлении", s.ID)
	}
	return nil
}

func (p *Postgres) Delete(ctx context.Context, id string) error {
	log.Printf("удаляю подписку %s", id)
	res, err := p.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=$1`, id)
	if err != nil {
		log.Printf("ошибка при удалении %s: %v", id, err)
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		log.Printf("подписка %s не найдена при удалении", id)
	}
	return nil
}

func (p *Postgres) List(ctx context.Context) ([]Sub, error) {
	log.Println("запрашивают список всех подписок")
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, name, price, user_id, start_date, end_date FROM subscriptions ORDER BY start_date DESC LIMIT 100`)
	if err != nil {
		log.Printf("ошибка при получении списка: %v", err)
		return nil, err
	}
	defer rows.Close()
	var list []Sub
	for rows.Next() {
		var s Sub
		var end sql.NullString
		rows.Scan(&s.ID, &s.Name, &s.Price, &s.User, &s.Start, &end)
		if end.Valid {
			s.End = &end.String
		}
		list = append(list, s)
	}
	log.Printf("отправляю %d подписок", len(list))
	return list, nil
}

func (p *Postgres) Sum(ctx context.Context, from, to, user, name string) (int, error) {
	log.Printf("считаю сумму за %s - %s", from, to)
	if user != "" {
		log.Printf("фильтр по юзеру %s", user)
	}
	if name != "" {
		log.Printf("фильтр по сервису %s", name)
	}

	q := `SELECT price, start_date, end_date FROM subscriptions WHERE 1=1`
	var args []interface{}
	n := 1

	if user != "" {
		q += " AND user_id = $" + string(rune('0'+n))
		args = append(args, user)
		n++
	}
	if name != "" {
		q += " AND name = $" + string(rune('0'+n))
		args = append(args, name)
		n++
	}

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("ошибка запроса: %v", err)
		return 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var price int
		var start, end sql.NullString
		rows.Scan(&price, &start, &end)

		if start.String > to || (end.Valid && end.String < from) {
			continue
		}

		st, _ := time.Parse("01-2006", start.String)
		et := time.Now()
		if end.Valid {
			et, _ = time.Parse("01-2006", end.String)
		}
		ps, _ := time.Parse("01-2006", from)
		pe, _ := time.Parse("01-2006", to)

		if st.After(ps) {
			ps = st
		}
		if et.Before(pe) {
			pe = et
		}

		m := 0
		for d := ps; !d.After(pe); d = d.AddDate(0, 1, 0) {
			m++
		}
		total += price * m
	}
	log.Printf("итого: %d", total)
	return total, nil
}

type Handler struct {
	db Storage
}

func NewHandler(db Storage) *Handler {
	return &Handler{db: db}
}

func (h *Handler) Add(w http.ResponseWriter, r *http.Request) {
	log.Println("кто-то создает подписку")
	var req SubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("ошибка парсинга: %v", err)
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Price <= 0 || req.User == "" || req.Start == "" {
		log.Println("не все поля заполнены")
		http.Error(w, "not all fields", http.StatusBadRequest)
		return
	}
	if _, err := uuid.Parse(req.User); err != nil {
		log.Printf("плохой user_id: %s", req.User)
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if !okDate(req.Start) {
		log.Printf("плохая дата начала: %s", req.Start)
		http.Error(w, "bad start date", http.StatusBadRequest)
		return
	}
	if req.End != nil && !okDate(*req.End) {
		log.Printf("плохая дата конца: %s", *req.End)
		http.Error(w, "bad end date", http.StatusBadRequest)
		return
	}

	s := &Sub{
		ID:    uuid.New().String(),
		Name:  req.Name,
		Price: req.Price,
		User:  req.User,
		Start: req.Start,
		End:   req.End,
	}

	if err := h.db.Create(r.Context(), s); err != nil {
		log.Printf("ошибка БД: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	log.Printf("создана подписка %s", s.ID)
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) GetOne(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	log.Printf("запрос подписки %s", id)
	s, err := h.db.Get(r.Context(), id)
	if err != nil {
		log.Printf("ошибка БД: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if s == nil {
		log.Printf("подписка %s не найдена", id)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) Upd(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	log.Printf("обновление подписки %s", id)
	var req SubReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("ошибка парсинга: %v", err)
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s := &Sub{
		ID:    id,
		Name:  req.Name,
		Price: req.Price,
		User:  req.User,
		Start: req.Start,
		End:   req.End,
	}
	if err := h.db.Update(r.Context(), s); err != nil {
		log.Printf("ошибка БД: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	log.Printf("подписка %s обновлена", id)
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) Del(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	log.Printf("удаление подписки %s", id)
	if err := h.db.Delete(r.Context(), id); err != nil {
		log.Printf("ошибка БД: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	log.Printf("подписка %s удалена", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) All(w http.ResponseWriter, r *http.Request) {
	log.Println("запрос списка подписок")
	list, err := h.db.List(r.Context())
	if err != nil {
		log.Printf("ошибка БД: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) Calc(w http.ResponseWriter, r *http.Request) {
	log.Println("запрос подсчета")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	user := r.URL.Query().Get("user")
	name := r.URL.Query().Get("name")

	if from == "" || to == "" {
		log.Println("нет from или to")
		http.Error(w, "need from and to", http.StatusBadRequest)
		return
	}

	total, err := h.db.Sum(r.Context(), from, to, user, name)
	if err != nil {
		log.Printf("ошибка подсчета: %v", err)
		http.Error(w, "calc error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]int{"total": total})
}

func okDate(d string) bool {
	if len(d) != 7 {
		return false
	}
	return d[2] == '-'
}

func main() {
	log.Println("запускаю сервер...")

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	pass := os.Getenv("DB_PASS")
	if pass == "" {
		pass = "0896"
	}
	name := os.Getenv("DB_NAME")
	if name == "" {
		name = "subscriptions"
	}
	appPort := os.Getenv("PORT")
	if appPort == "" {
		appPort = "8080"
	}

	conn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, name)

	db, err := sql.Open("postgres", conn)
	if err != nil {
		log.Fatal("db connect error:", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal("db ping error:", err)
	}
	log.Println("база данных подключена")

	store := NewPostgres(db)
	h := NewHandler(store)

	r := mux.NewRouter()
	r.HandleFunc("/subscriptions", h.Add).Methods("POST")
	r.HandleFunc("/subscriptions", h.All).Methods("GET")
	r.HandleFunc("/subscriptions/{id}", h.GetOne).Methods("GET")
	r.HandleFunc("/subscriptions/{id}", h.Upd).Methods("PUT")
	r.HandleFunc("/subscriptions/{id}", h.Del).Methods("DELETE")
	r.HandleFunc("/calc", h.Calc).Methods("GET")

	log.Println("проверяю роуты:")
	r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		path, _ := route.GetPathTemplate()
		methods, _ := route.GetMethods()
		log.Printf("  %s %v", path, methods)
		return nil
	})

	log.Println("роуты настроены")

	srv := &http.Server{Addr: ":" + appPort, Handler: r}
	go func() {
		log.Println("сервер запущен на порту", appPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("ошибка сервера:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("останавливаю сервер...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("сервер остановлен")
}
