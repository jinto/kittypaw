package relay

const (
	MsgRateLimited     = "일일 사용 한도에 도달했습니다."
	MsgNoCallback      = "KittyPaw 스킬 서버가 정상 동작 중입니다. 오픈빌더에서 비동기 콜백을 활성화하면 AI 응답을 받을 수 있습니다."
	MsgNotPaired       = "KittyPaw와 연결이 필요합니다. KittyPaw 앱에서 연결 코드를 확인하세요."
	MsgTransientError  = "일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요."
	MsgOffline         = "KittyPaw가 현재 오프라인 상태입니다. 앱을 실행 후 다시 시도해 주세요."
	MsgInvalidPairCode = "유효하지 않은 연결 코드입니다. KittyPaw 앱에서 새 코드를 확인하세요."
	MsgPaired          = "연결 완료!"
	MsgProcessing      = "처리 중입니다..."
)

type KakaoPayload struct {
	Action      KakaoAction      `json:"action"`
	UserRequest KakaoUserRequest `json:"userRequest"`
}

type KakaoAction struct {
	ID string `json:"id"`
}

type KakaoUserRequest struct {
	Utterance   string    `json:"utterance"`
	User        KakaoUser `json:"user"`
	CallbackURL *string   `json:"callbackUrl,omitempty"`
}

type KakaoUser struct {
	ID string `json:"id"`
}

type KakaoSimpleResponse struct {
	Version  string        `json:"version"`
	Template KakaoTemplate `json:"template"`
}

type KakaoTemplate struct {
	Outputs []KakaoOutput `json:"outputs"`
}

type KakaoOutput struct {
	SimpleText  *KakaoSimpleText  `json:"simpleText,omitempty"`
	SimpleImage *KakaoSimpleImage `json:"simpleImage,omitempty"`
}

type KakaoSimpleText struct {
	Text string `json:"text"`
}

type KakaoSimpleImage struct {
	ImageURL string `json:"imageUrl"`
	AltText  string `json:"altText"`
}

type KakaoAsyncAck struct {
	Version     string         `json:"version"`
	UseCallback bool           `json:"useCallback"`
	Data        KakaoAsyncData `json:"data"`
}

type KakaoAsyncData struct {
	Text string `json:"text"`
}

func Text(text string) KakaoSimpleResponse {
	return KakaoSimpleResponse{
		Version: "2.0",
		Template: KakaoTemplate{
			Outputs: []KakaoOutput{{
				SimpleText: &KakaoSimpleText{Text: text},
			}},
		},
	}
}

func Image(imageURL, altText string) KakaoSimpleResponse {
	return KakaoSimpleResponse{
		Version: "2.0",
		Template: KakaoTemplate{
			Outputs: []KakaoOutput{{
				SimpleImage: &KakaoSimpleImage{
					ImageURL: imageURL,
					AltText:  altText,
				},
			}},
		},
	}
}

func AsyncAck() KakaoAsyncAck {
	return KakaoAsyncAck{
		Version:     "2.0",
		UseCallback: true,
		Data:        KakaoAsyncData{Text: MsgProcessing},
	}
}

type WSOutgoing struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	UserID string `json:"user_id"`
}

type WSIncoming struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url,omitempty"`
	ImageAlt string `json:"image_alt,omitempty"`
}

type PendingContext struct {
	CallbackURL string
	UserID      string
	CreatedAt   int64
}

type RegisterResponse struct {
	Token      string `json:"token"`
	PairCode   string `json:"pair_code"`
	ChannelURL string `json:"channel_url"`
}

type PairStatusResponse struct {
	Paired bool `json:"paired"`
}

type AdminStatsResponse struct {
	Daily      LimitInfo `json:"daily"`
	Monthly    LimitInfo `json:"monthly"`
	Killswitch bool      `json:"killswitch"`
	WSSessions int       `json:"ws_sessions"`
	RSSBytes   uint64    `json:"rss_bytes"`
	FDCount    uint64    `json:"fd_count"`
}

type LimitInfo struct {
	Current uint64 `json:"current"`
	Limit   uint64 `json:"limit"`
}

type KillswitchResponse struct {
	Killswitch bool `json:"killswitch"`
}

type RateLimitResult struct {
	OK      bool
	Daily   uint64
	Monthly uint64
}

type Stats struct {
	Daily   uint64
	Monthly uint64
}
