package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/synctv-org/synctv/internal/db"
	dbModel "github.com/synctv-org/synctv/internal/model"
	"github.com/synctv-org/synctv/internal/op"
	"github.com/synctv-org/synctv/internal/settings"
	"github.com/synctv-org/synctv/server/middlewares"
	"github.com/synctv-org/synctv/server/model"
	"github.com/synctv-org/synctv/utils"
	"gorm.io/gorm"
)

var (
	ErrAuthFailed  = errors.New("auth failed")
	ErrAuthExpired = errors.New("auth expired")
	ErrRoomAlready = errors.New("room already exists")
)

type FormatErrNotSupportPosition string

func (e FormatErrNotSupportPosition) Error() string {
	return fmt.Sprintf("not support position %s", string(e))
}

func CreateRoom(ctx *gin.Context) {
	user := ctx.MustGet("user").(*op.User)

	if settings.DisableCreateRoom.Get() && !user.IsAdmin() {
		ctx.AbortWithStatusJSON(http.StatusForbidden, model.NewApiErrorStringResp("create room is disabled"))
		return
	}

	req := model.CreateRoomReq{}
	if err := model.Decode(ctx, &req); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	CreateRoomNeedReview := settings.CreateRoomNeedReview.Get()

	var (
		r   *dbModel.Room
		err error
	)
	if CreateRoomNeedReview {
		r, err = user.CreateRoom(req.RoomName, req.Password, db.WithSetting(req.Setting), db.WithStatus(dbModel.RoomStatusPending))
	} else {
		r, err = user.CreateRoom(req.RoomName, req.Password, db.WithSetting(req.Setting), db.WithStatus(dbModel.RoomStatusActive))
	}
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	room, _ := op.LoadOrInitRoomByID(r.ID)

	token, err := middlewares.NewAuthRoomToken(user, room)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, model.NewApiErrorResp(err))
		return
	}

	ctx.JSON(http.StatusCreated, model.NewApiDataResp(gin.H{
		"roomId": room.ID,
		"token":  token,
	}))
}

func RoomHotList(ctx *gin.Context) {
	page, pageSize, err := GetPageAndPageSize(ctx)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	r := op.GetRoomHeapInCacheWithoutHidden()
	rs := utils.GetPageItems(r, page, pageSize)
	resp := make([]*model.RoomListResp, len(rs))
	for i, v := range rs {
		resp[i] = &model.RoomListResp{
			RoomId:       v.ID,
			RoomName:     v.RoomName,
			PeopleNum:    v.ClientNum,
			NeedPassword: v.NeedPassword,
			Creator:      op.GetUserName(v.CreatorID),
			CreatedAt:    v.CreatedAt.UnixMilli(),
		}
	}

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"total": len(r),
		"list":  rs,
	}))
}

func RoomList(ctx *gin.Context) {
	order := ctx.Query("order")
	if order == "" {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorStringResp("order is required"))
		return
	}
	page, pageSize, err := GetPageAndPageSize(ctx)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	var desc = ctx.DefaultQuery("sort", "desc") == "desc"

	// search mode, all, name, creator
	var search = ctx.DefaultQuery("search", "all")

	scopes := []func(db *gorm.DB) *gorm.DB{}

	switch order {
	case "createdAt":
		if desc {
			scopes = append(scopes, db.OrderByCreatedAtDesc)
		} else {
			scopes = append(scopes, db.OrderByCreatedAtAsc)
		}
		if keyword := ctx.Query("keyword"); keyword != "" {
			switch search {
			case "all":
				scopes = append(scopes, db.WhereRoomNameLikeOrCreatorIn(keyword, db.GerUsersIDByUsernameLike(keyword)))
			case "name":
				scopes = append(scopes, db.WhereRoomNameLike(keyword))
			case "creator":
				scopes = append(scopes, db.WhereCreatorIDIn(db.GerUsersIDByUsernameLike(keyword)))
			}
		}
	case "roomName":
		if desc {
			scopes = append(scopes, db.OrderByDesc("name"))
		} else {
			scopes = append(scopes, db.OrderByAsc("name"))
		}
		if keyword := ctx.Query("keyword"); keyword != "" {
			switch search {
			case "all":
				scopes = append(scopes, db.WhereRoomNameLikeOrCreatorIn(keyword, db.GerUsersIDByUsernameLike(keyword)))
			case "name":
				scopes = append(scopes, db.WhereRoomNameLike(keyword))
			case "creator":
				scopes = append(scopes, db.WhereCreatorIDIn(db.GerUsersIDByUsernameLike(keyword)))
			}
		}
	case "roomId":
		if desc {
			scopes = append(scopes, db.OrderByIDDesc)
		} else {
			scopes = append(scopes, db.OrderByIDAsc)
		}
		if keyword := ctx.Query("keyword"); keyword != "" {
			switch search {
			case "all":
				scopes = append(scopes, db.WhereRoomNameLikeOrCreatorIn(keyword, db.GerUsersIDByUsernameLike(keyword)))
			case "name":
				scopes = append(scopes, db.WhereRoomNameLike(keyword))
			case "creator":
				scopes = append(scopes, db.WhereCreatorIDIn(db.GerUsersIDByUsernameLike(keyword)))
			}
		}
	default:
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorStringResp("not support order"))
		return
	}

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"total": db.GetAllRoomsWithoutHiddenCount(scopes...),
		"list":  genRoomListResp(append(scopes, db.Paginate(page, pageSize))...),
	}))
}

func genRoomListResp(scopes ...func(db *gorm.DB) *gorm.DB) []*model.RoomListResp {
	rs := db.GetAllRoomsWithoutHidden(scopes...)
	resp := make([]*model.RoomListResp, len(rs))
	for i, r := range rs {
		resp[i] = &model.RoomListResp{
			RoomId:       r.ID,
			RoomName:     r.Name,
			PeopleNum:    op.ClientNum(r.ID),
			NeedPassword: len(r.HashedPassword) != 0,
			Creator:      op.GetUserName(r.CreatorID),
			CreatedAt:    r.CreatedAt.UnixMilli(),
		}
	}
	return resp
}

func CheckRoom(ctx *gin.Context) {
	r, err := db.GetRoomByID(ctx.Query("roomId"))
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusNotFound, model.NewApiErrorResp(err))
		return
	}

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"peopleNum":    op.ClientNum(r.ID),
		"needPassword": r.NeedPassword(),
	}))
}

func LoginRoom(ctx *gin.Context) {
	user := ctx.MustGet("user").(*op.User)

	req := model.LoginRoomReq{}
	if err := model.Decode(ctx, &req); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	room, err := op.LoadOrInitRoomByID(req.RoomId)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusNotFound, model.NewApiErrorResp(err))
		return
	}

	if room.CreatorID != user.ID && !room.CheckPassword(req.Password) {
		ctx.AbortWithStatusJSON(http.StatusForbidden, model.NewApiErrorStringResp("password error"))
		return
	}

	token, err := middlewares.NewAuthRoomToken(user, room)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, model.NewApiErrorResp(err))
		return
	}

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"roomId": room.ID,
		"token":  token,
	}))
}

func DeleteRoom(ctx *gin.Context) {
	room := ctx.MustGet("room").(*op.Room)
	user := ctx.MustGet("user").(*op.User)

	if err := user.DeleteRoom(room.ID); err != nil {
		ctx.AbortWithStatusJSON(http.StatusForbidden, model.NewApiErrorResp(err))
		return
	}

	ctx.Status(http.StatusNoContent)
}

func SetRoomPassword(ctx *gin.Context) {
	room := ctx.MustGet("room").(*op.Room)
	user := ctx.MustGet("user").(*op.User)

	req := model.SetRoomPasswordReq{}
	if err := model.Decode(ctx, &req); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, model.NewApiErrorResp(err))
		return
	}

	if err := user.SetRoomPassword(room.ID, req.Password); err != nil {
		ctx.AbortWithStatusJSON(http.StatusForbidden, model.NewApiErrorResp(err))
		return
	}

	token, err := middlewares.NewAuthRoomToken(user, room)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, model.NewApiErrorResp(err))
		return
	}

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"roomId": room.ID,
		"token":  token,
	}))
}

func RoomSetting(ctx *gin.Context) {
	room := ctx.MustGet("room").(*op.Room)
	// user := ctx.MustGet("user").(*op.User)

	ctx.JSON(http.StatusOK, model.NewApiDataResp(gin.H{
		"hidden":       room.Settings.Hidden,
		"needPassword": room.NeedPassword(),
	}))
}
