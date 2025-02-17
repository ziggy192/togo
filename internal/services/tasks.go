package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/manabie-com/togo/internal/storages"
)

// ToDoService implement HTTP server
type ToDoService struct {
	JWTKey string
	Store  storages.DB
}

func (s *ToDoService) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	log.Println(req.Method, req.URL.Path)
	resp.Header().Set("Access-Control-Allow-Origin", "*")
	resp.Header().Set("Access-Control-Allow-Headers", "*")
	resp.Header().Set("Access-Control-Allow-Methods", "*")

	if req.Method == http.MethodOptions {
		resp.WriteHeader(http.StatusOK)
		return
	}

	switch req.URL.Path {
	case "/login":
		s.getAuthToken(resp, req)
		return
	case "/tasks":
		var ok bool
		req, ok = s.validToken(req)
		if !ok {
			resp.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch req.Method {
		case http.MethodGet:
			s.listTasks(resp, req)
		case http.MethodPost:
			s.addTask(resp, req)
		}
		return
	}
}

func (s *ToDoService) getAuthToken(resp http.ResponseWriter, req *http.Request) {
	id := value(req, "user_id")
	if !s.Store.ValidateUser(req.Context(), id, value(req, "password")) {
		responseError(resp, http.StatusUnauthorized, "incorrect user_id/pwd")
		return
	}
	token, err := s.createToken(id.String)
	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(resp, token)
}

func (s *ToDoService) listTasks(resp http.ResponseWriter, req *http.Request) {
	id, _ := userIDFromCtx(req.Context())
	tasks, err := s.Store.RetrieveTasks(
		req.Context(),
		sql.NullString{
			String: id,
			Valid:  true,
		},
		value(req, "created_date"),
	)

	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}

	responseOK(resp, tasks)
}

func (s *ToDoService) addTask(resp http.ResponseWriter, req *http.Request) {
	userID, _ := userIDFromCtx(req.Context())
	user, err := s.Store.RetrieveUser(req.Context(), userID)
	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	formattedNow := now.Format("2006-01-02")

	count, err := s.Store.CountTasks(req.Context(), userID, formattedNow)
	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}

	if int(count.Int32) >= user.MaxTodo {
		responseError(resp, http.StatusBadRequest, fmt.Sprintf("Limited to %d tasks per day", user.MaxTodo))
		return
	}

	t := &storages.Task{}
	err = json.NewDecoder(req.Body).Decode(t)
	defer req.Body.Close()
	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}

	t.ID = uuid.New().String()
	t.UserID = userID
	t.CreatedDate = formattedNow

	err = s.Store.AddTask(req.Context(), t)
	if err != nil {
		responseError(resp, http.StatusInternalServerError, err.Error())
		return
	}

	responseOK(resp, t)
}

func value(req *http.Request, p string) sql.NullString {
	return sql.NullString{
		String: req.FormValue(p),
		Valid:  true,
	}
}

func (s *ToDoService) createToken(id string) (string, error) {
	atClaims := jwt.MapClaims{}
	atClaims["user_id"] = id
	atClaims["exp"] = time.Now().Add(time.Minute * 15).Unix()
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, atClaims)
	token, err := at.SignedString([]byte(s.JWTKey))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *ToDoService) validToken(req *http.Request) (*http.Request, bool) {
	token := req.Header.Get("Authorization")

	claims := make(jwt.MapClaims)
	t, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(s.JWTKey), nil
	})
	if err != nil {
		log.Println(err)
		return req, false
	}

	if !t.Valid {
		return req, false
	}

	id, ok := claims["user_id"].(string)
	if !ok {
		return req, false
	}

	req = req.Clone(context.WithValue(req.Context(), userAuthKey(0), id))
	return req, true
}

type userAuthKey int8

func userIDFromCtx(ctx context.Context) (string, bool) {
	v := ctx.Value(userAuthKey(0))
	id, ok := v.(string)
	return id, ok
}
