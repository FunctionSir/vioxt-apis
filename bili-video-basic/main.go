/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2026-05-22 21:01:40
 * @LastEditTime: 2026-05-25 22:11:29
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /bili-video-basic/main.go
 */

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ErrResp struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
}

type BiliVideoBasic struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID               string `json:"bvid"`
		AID                uint64 `json:"aid"`
		Cover              string `json:"pic"`
		Title              string `json:"title"`
		PublishAtTimestamp int64  `json:"pubdate"`
		Desc               string `json:"desc"`
		UP                 struct {
			UID      uint64 `json:"mid"`
			Nickname string `json:"name"`
			Avatar   string `json:"face"`
		} `json:"owner"`
		Stats struct {
			View     uint64 `json:"view"`
			Danmaku  uint64 `json:"danmaku"`
			Comment  uint64 `json:"reply"`
			Favorite uint64 `json:"favorite"`
			Coin     uint64 `json:"coin"`
			Share    uint64 `json:"share"`
			Like     uint64 `json:"like"`
		} `json:"stat"`
		Parts []struct {
			CID               uint64 `json:"cid"`
			PartNumber        uint64 `json:"page"`
			PartTitle         string `json:"part"`
			DurationAsSeconds uint64 `json:"duration"`
		} `json:"pages"`
	} `json:"data"`
}

type BiliVideoBasicResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID      string    `json:"bvid"`
		AID       uint64    `json:"aid"`
		Cover     string    `json:"cover"`
		Title     string    `json:"title"`
		PublishAt time.Time `json:"publish_at"`
		Desc      string    `json:"desc"`
		UP        struct {
			UID      uint64 `json:"uid"`
			Nickname string `json:"nickname"`
			Avatar   string `json:"avatar"`
		} `json:"up"`
		Stats struct {
			View     uint64 `json:"view"`
			Danmaku  uint64 `json:"danmaku"`
			Comment  uint64 `json:"reply"`
			Favorite uint64 `json:"favorite"`
			Coin     uint64 `json:"coin"`
			Share    uint64 `json:"share"`
			Like     uint64 `json:"like"`
		} `json:"stats"`
		Parts []struct {
			CID        uint64 `json:"cid"`
			PartNumber uint64 `json:"part_number"`
			PartTitle  string `json:"part_title"`
			DurationS  uint64 `json:"duration_s"`
			DanmakuXML string `json:"danmaku_xml"`
		} `json:"parts"`
	} `json:"data"`
}

//go:embed useragents.txt
var UAsInTxt string
var UAs []string

const HTTPHeaderKeyUA string = "User-Agent"
const RespInternalServerError string = `{"code":500,"message":"internal_server_error"}`
const HTTPClientTimeout time.Duration = 60 * time.Second

const BiliAPIBaseURL string = "https://api.bilibili.com/x/web-interface/view"
const BiliDanmakuXMLBaseURL string = "https://api.bilibili.com/x/v1/dm/list.so?oid="

func BiliRespToVioxtResp(from *BiliVideoBasic, to *BiliVideoBasicResp) {
	if from.Code == 0 {
		to.Code = http.StatusOK
	} else {
		to.Code = from.Code
	}
	if to.Code == http.StatusOK {
		to.Message = "ok"
	} else {
		to.Message = from.Message
	}
	to.Data.AID = from.Data.AID
	to.Data.BVID = from.Data.BVID
	to.Data.Cover = from.Data.Cover
	to.Data.Title = from.Data.Title
	to.Data.PublishAt = time.Unix(from.Data.PublishAtTimestamp, 0).UTC()
	to.Data.Desc = from.Data.Desc
	to.Data.UP = struct {
		UID      uint64 `json:"uid"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	}(from.Data.UP)
	to.Data.Stats = from.Data.Stats
	to.Data.Parts = make([]struct {
		CID        uint64 `json:"cid"`
		PartNumber uint64 `json:"part_number"`
		PartTitle  string `json:"part_title"`
		DurationS  uint64 `json:"duration_s"`
		DanmakuXML string `json:"danmaku_xml"`
	}, 0)
	for _, part := range from.Data.Parts {
		var respPart struct {
			CID        uint64 `json:"cid"`
			PartNumber uint64 `json:"part_number"`
			PartTitle  string `json:"part_title"`
			DurationS  uint64 `json:"duration_s"`
			DanmakuXML string `json:"danmaku_xml"`
		}
		respPart.CID = part.CID
		respPart.PartNumber = part.PartNumber
		respPart.PartTitle = part.PartTitle
		respPart.DurationS = part.DurationAsSeconds
		respPart.DanmakuXML = BiliDanmakuXMLBaseURL + strconv.FormatUint(part.CID, 10)
		to.Data.Parts = append(to.Data.Parts, respPart)
	}
}

func VideoBasicHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	defer func() { log.Println("Request", r.URL.String(), "processed in", time.Since(now)) }()
	aid := r.URL.Query().Get("aid")
	bvid := r.URL.Query().Get("bvid")
	pretty := r.URL.Query().Get("pretty")

	w.Header().Set("Content-Type", "application/json")

	// If empty aid and bvid found.
	if len(aid) <= 0 && len(bvid) <= 0 {
		resp, err := json.Marshal(ErrResp{http.StatusBadRequest, "no_aid_or_bvid_provided"})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(resp)
	}

	// Parse parameter pretty.
	var prettyRespRequired bool
	switch pretty {
	case "", "false", "0":
		prettyRespRequired = false
	case "true", "1":
		prettyRespRequired = true
	default:
		resp, err := json.Marshal(ErrResp{http.StatusBadRequest, "invalid_value_for_pretty"})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(resp)
		return
	}

	// Construct URL.
	reqURL, err := url.Parse(BiliAPIBaseURL)
	if err != nil {
		log.Println("Error occurred while processing a request:", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(RespInternalServerError))
		return
	}
	parms := url.Values{}
	if len(bvid) > 0 {
		parms.Set("bvid", bvid)
	} else {
		parms.Set("aid", aid)
	}
	reqURL.RawQuery = parms.Encode()

	// Construct request.
	req, err := http.NewRequest("GET", reqURL.String(), nil)
	if err != nil {
		log.Println("Error occurred while processing a request:", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(RespInternalServerError))
		return
	}
	req.Header.Set(HTTPHeaderKeyUA, UAs[rand.IntN(len(UAs))])

	biliResp, err := http.DefaultClient.Do(req)
	if err != nil {
		if urlErr, ok := err.(*url.Error); ok {
			switch {
			case urlErr.Timeout():
				resp, err := json.Marshal(ErrResp{http.StatusGatewayTimeout, "request_to_upstream_timeout"})
				if err != nil {
					log.Println("Error occurred while processing a request:", err)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(RespInternalServerError))
					return
				}
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write(resp)
				return
			case urlErr.Temporary():
				resp, err := json.Marshal(ErrResp{http.StatusGatewayTimeout, "temporary_not_timeout_error"})
				if err != nil {
					log.Println("Error occurred while processing a request:", err)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(RespInternalServerError))
					return
				}
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write(resp)
				return
			default:
				log.Println("Error occurred while processing a request:", err)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(RespInternalServerError))
				return
			}
		} else {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
	}
	defer func() { _ = biliResp.Body.Close() }()
	var biliApiRes BiliVideoBasic
	biliApiDecoder := json.NewDecoder(biliResp.Body)
	err = biliApiDecoder.Decode(&biliApiRes)
	if err != nil {
		log.Println("Error occurred while processing a request:", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(RespInternalServerError))
		return
	}
	switch biliApiRes.Code {
	case 62002:
		resp, err := json.Marshal(ErrResp{http.StatusForbidden, "video_unavailable"})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(resp)
		return
	case -404:
		resp, err := json.Marshal(ErrResp{http.StatusNotFound, "video_not_found"})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(resp)
		return
	case -400:
		resp, err := json.Marshal(ErrResp{http.StatusBadRequest, "invalid_request_parameters"})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(resp)
		return
	}
	if biliApiRes.Code != 0 {
		resp, err := json.Marshal(ErrResp{biliApiRes.Code, "not_acceptable_upstream_response::" + biliApiRes.Message})
		if err != nil {
			log.Println("Error occurred while processing a request:", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(RespInternalServerError))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(resp)
	}
	var vioxtRespStruct BiliVideoBasicResp
	BiliRespToVioxtResp(&biliApiRes, &vioxtRespStruct)
	var resp []byte
	if prettyRespRequired {
		resp, err = json.MarshalIndent(vioxtRespStruct, "", "  ") // Do NOT use := here!
	} else {
		resp, err = json.Marshal(vioxtRespStruct)
	}
	if err != nil {
		log.Println("Error occurred while processing a request:", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(RespInternalServerError))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func main() {
	fmt.Println("BiliBili Video Basic API Backend")
	fmt.Println("Version: 0.1.0")
	if len(os.Args) <= 1 {
		panic("no enough args")
	}
	for ua := range strings.SplitSeq(UAsInTxt, "\n") {
		uaProcessed := strings.TrimSpace(ua)
		if len(uaProcessed) <= 0 {
			continue
		}
		UAs = append(UAs, uaProcessed)
	}
	http.DefaultClient.Timeout = HTTPClientTimeout
	http.HandleFunc("GET /", VideoBasicHandler)
	err := http.ListenAndServe(os.Args[1], nil)
	panic(err)
}
