package msgid

// internal/market/client.go、internal/market/errors.go
const (
	MarketNoToken                = "market.no_token"
	MarketHintCheckMarketVersion = "market.hint.check_market_version"
	MarketBadURL                 = "market.bad_url"
	MarketHintCheckMarketURL     = "market.hint.check_market_url"
	MarketReadResponseFailed     = "market.read_response_failed"
	MarketHintCheckNetworkRetry  = "market.hint.check_network_retry"
	MarketBodyUnparseable        = "market.body_unparseable"
	MarketHintCheckAPIBase       = "market.hint.check_api_base"
	MarketBodyShapeMismatch      = "market.body_shape_mismatch"
	MarketUnreachable            = "market.unreachable"
	MarketHintCheckNetworkAndURL = "market.hint.check_network_and_url"

	// 服务端没给 message 时的兜底文案（给了就原样用服务端的）。
	MarketFallbackBlocked       = "market.fallback.blocked"
	MarketFallbackUnauthorized  = "market.fallback.unauthorized"
	MarketFallbackForbidden     = "market.fallback.forbidden"
	MarketFallbackVersionExists = "market.fallback.version_exists"
	MarketFallbackNotFound      = "market.fallback.not_found"
	MarketFallbackNoReason      = "market.fallback.no_reason"

	MarketHintBlockedNoInstall   = "market.hint.blocked_no_install"
	MarketHintBlockedPickAnother = "market.hint.blocked_pick_another"
	MarketHintLogin              = "market.hint.login"
	MarketHintSetAuthToken       = "market.hint.set_auth_token"
	MarketHintCheckOwner         = "market.hint.check_owner"
	MarketHintPrivateNeedsGrant  = "market.hint.private_needs_grant"
	MarketHintVersionNotReusable = "market.hint.version_not_reusable"
	MarketHintCheckIDAndVersion  = "market.hint.check_id_and_version"
	MarketHintServerFault        = "market.hint.server_fault"
	MarketHintFixReasonAndRetry  = "market.hint.fix_reason_and_retry"

	MarketActionFailed = "market.action_failed"

	MarketStatusOnly     = "market.status_only"
	MarketStatusWithBody = "market.status_with_body"
	MarketDescribeJoin   = "market.describe_join"

	// c.do(...) 的 action 参数：拼进"错误：%s失败"里，所以都写成动名词短语。
	MarketActionLogin          = "market.action.login"
	MarketActionLogout         = "market.action.logout"
	MarketActionPublishVersion = "market.action.publish_version"
	MarketActionListVersions   = "market.action.list_versions"
	MarketActionFetchManifest  = "market.action.fetch_manifest"
	MarketActionListArtifacts  = "market.action.list_artifacts"
	MarketActionUploadArtifact = "market.action.upload_artifact"
	MarketActionSetStatus      = "market.action.set_status"
	MarketActionSetVisibility  = "market.action.set_visibility"
)

// internal/source/market.go 用到的动作名
const (
	MarketActionAccess = "market.action.access"
)
