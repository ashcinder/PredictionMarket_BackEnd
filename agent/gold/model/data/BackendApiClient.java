package com.example.brokerfi.xc.agent.gold.model.data;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import com.example.brokerfi.BuildConfig;
import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;
import com.google.gson.reflect.TypeToken;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.OutputStream;
import java.io.InputStream;
import java.lang.reflect.Type;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Scanner;

/**
 * BackendApiClient - Go 后端数据库交互客户端
 *
 * 职责：DApp 与 Go 后端 PostgreSQL 数据库之间的桥梁。
 *
 * 设计原则（减少链交互时延）：
 * - 读操作：优先从后端 DB 读取缓存数据（快速），失败时回退到 IPFS/链上直读
 * - 写操作：先完成 IPFS 上传 + 链上交易，然后同步元数据到后端 DB
 *
 * 后端 API 基础路径：{BASE_URL}/api/v1/gold/
 *
 * URL 自动切换规则：
 * - Debug 构建：http://10.0.2.2:8081  （Android 模拟器 → 宿主机 localhost）
 * - Release 构建：https://dash.broker-chain.com:440 （生产服务器）
 * - 可手动覆盖：SharedPreferences "backend_prefs" → "base_url"
 */
public class BackendApiClient {
    private static final String TAG = "BackendApiClient";

    // ── URL 配置 ──
    private static final String PROD_BASE_URL = "https://dash.broker-chain.com:440";
    // Android 模拟器中 10.0.2.2 = 宿主机 localhost
    // 真机调试请改为电脑局域网 IP，如 http://192.168.1.100:8081
    private static final String DEV_BASE_URL = "http://10.0.2.2:8081";
    private static final String PREFS_NAME = "backend_prefs";
    private static final String KEY_BASE_URL = "base_url";

    private static final String API_PREFIX = "/api/v1/gold";

    private static final Gson gson = new Gson();
    private static final int CONNECT_TIMEOUT_MS = 8000;
    private static final int READ_TIMEOUT_MS = 10000;

    private static String cachedBaseUrl = null;

    /**
     * 获取当前生效的 Base URL
     * 优先级：手动设置 > Debug/Release 自动选择
     */
    public static String getBaseUrl(Context ctx) {
        if (cachedBaseUrl != null) return cachedBaseUrl;

        // 1. 检查是否有手动设置的 URL（开发调试用）
        if (ctx != null) {
            SharedPreferences prefs = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
            String manual = prefs.getString(KEY_BASE_URL, null);
            if (manual != null && !manual.trim().isEmpty()) {
                cachedBaseUrl = manual.trim();
                Log.d(TAG, "使用手动设置的 Base URL: " + cachedBaseUrl);
                return cachedBaseUrl;
            }
        }

        // 2. 根据构建类型自动选择
        if (BuildConfig.DEBUG) {
            cachedBaseUrl = DEV_BASE_URL;
        } else {
            cachedBaseUrl = PROD_BASE_URL;
        }
        Log.d(TAG, "Base URL 自动选择: " + cachedBaseUrl + " (DEBUG=" + BuildConfig.DEBUG + ")");
        return cachedBaseUrl;
    }

    /**
     * 手动设置 Base URL（用于开发调试切换服务器）
     */
    public static void setBaseUrl(Context ctx, String url) {
        cachedBaseUrl = (url != null && !url.trim().isEmpty()) ? url.trim() : null;
        if (ctx != null) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                    .edit().putString(KEY_BASE_URL, cachedBaseUrl).apply();
        }
        Log.d(TAG, "Base URL 已更新: " + cachedBaseUrl);
    }

    /**
     * 清除手动设置，恢复自动选择
     */
    public static void resetBaseUrl(Context ctx) {
        cachedBaseUrl = null;
        if (ctx != null) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                    .edit().remove(KEY_BASE_URL).apply();
        }
        Log.d(TAG, "Base URL 已重置为自动选择");
    }

    // ==================== 内部 HTTP 工具 ====================

    private static String resolveBaseUrl() {
        // 无 Context 时的回退：优先 dev，因为这里主要被 GoldMarketRepository 调用
        // GoldMarketRepository 有 Context，调用前会通过 getBaseUrl(ctx) 触发缓存
        if (cachedBaseUrl != null) return cachedBaseUrl;
        return BuildConfig.DEBUG ? DEV_BASE_URL : PROD_BASE_URL;
    }

    private static String doGet(String path) throws Exception {
        String base = resolveBaseUrl();
        String fullUrl = base + API_PREFIX + path;
        Log.d(TAG, "GET " + fullUrl);
        URL url = new URL(fullUrl);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("GET");
        conn.setRequestProperty("Accept", "application/json");
        conn.setConnectTimeout(CONNECT_TIMEOUT_MS);
        conn.setReadTimeout(READ_TIMEOUT_MS);

        int code = conn.getResponseCode();
        if (code >= 200 && code < 300) {
            String body = readStream(conn.getInputStream());
            Log.d(TAG, "GET " + path + " -> 200 OK (" + body.length() + " bytes)");
            return body;
        } else {
            String err = readStream(conn.getErrorStream());
            Log.w(TAG, "GET " + fullUrl + " -> HTTP " + code + ": " + err);
            throw new Exception("Backend GET " + path + " failed: HTTP " + code + " - " + err);
        }
    }

    private static String doPost(String path, String jsonBody) throws Exception {
        String base = resolveBaseUrl();
        String fullUrl = base + API_PREFIX + path;
        Log.d(TAG, "POST " + fullUrl);
        URL url = new URL(fullUrl);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setRequestProperty("Content-Type", "application/json; charset=UTF-8");
        conn.setRequestProperty("Accept", "application/json");
        conn.setDoOutput(true);
        conn.setConnectTimeout(CONNECT_TIMEOUT_MS);
        conn.setReadTimeout(READ_TIMEOUT_MS);

        try (OutputStream os = conn.getOutputStream()) {
            os.write(jsonBody.getBytes(StandardCharsets.UTF_8));
        }

        int code = conn.getResponseCode();
        if (code >= 200 && code < 300) {
            String body = readStream(conn.getInputStream());
            Log.d(TAG, "POST " + path + " -> 200 OK");
            return body;
        } else {
            String err = readStream(conn.getErrorStream());
            Log.w(TAG, "POST " + fullUrl + " -> HTTP " + code + ": " + err);
            throw new Exception("Backend POST " + path + " failed: HTTP " + code + " - " + err);
        }
    }

    private static String readStream(InputStream stream) {
        if (stream == null) return "";
        try (Scanner scanner = new Scanner(stream, "UTF-8").useDelimiter("\\A")) {
            return scanner.hasNext() ? scanner.next() : "";
        }
    }

    // ==================== 游戏元数据 API（DB 读写） ====================

    /**
     * 从后端 DB 获取所有游戏的元数据（标题、条件、图片等）
     * 相比从 IPFS 逐个下载，DB 查询快得多
     *
     * GET /api/v1/gold/games
     * Response: { "games": [ GameMetaDTO, ... ] }
     */
    public static List<GameMetaDTO> fetchAllGameMetadata() throws Exception {
        String body = doGet("/games");
        JSONObject json = new JSONObject(body);
        JSONArray arr = json.getJSONArray("games");
        Type listType = new TypeToken<List<GameMetaDTO>>(){}.getType();
        return gson.fromJson(arr.toString(), listType);
    }

    /**
     * 从后端 DB 获取单个游戏的元数据
     *
     * GET /api/v1/gold/games/{gameId}
     * Response: GameMetaDTO
     */
    public static GameMetaDTO fetchGameMetadata(int gameId) throws Exception {
        String body = doGet("/games/" + gameId);
        return gson.fromJson(body, GameMetaDTO.class);
    }

    /**
     * 同步游戏元数据到后端 DB（创建或更新）
     * 调用时机：创建游戏完成后（IPFS 已上传 + 链上交易已确认）
     *
     * POST /api/v1/gold/games/sync
     * Body: GameMetaSyncReq
     * Response: { "success": true, "game_id": 123 }
     */
    public static boolean syncGameMetadata(GameMetaSyncReq req) throws Exception {
        String body = doPost("/games/sync", gson.toJson(req));
        JSONObject json = new JSONObject(body);
        return json.optBoolean("success", false);
    }

    // ==================== 链上状态缓存 API ====================

    /**
     * 从后端 DB 获取缓存的链上状态（避免直接 eth_call 的延迟）
     *
     * GET /api/v1/gold/games/{gameId}/chain-state
     * Response: ChainStateDTO
     */
    public static ChainStateDTO fetchChainState(int gameId, String userAddress) throws Exception {
        String body = doGet("/games/" + gameId + "/chain-state?user_address=" + userAddress);
        return gson.fromJson(body, ChainStateDTO.class);
    }

    /**
     * 批量获取所有游戏的链上缓存状态
     *
     * GET /api/v1/gold/games/chain-states?user_address=0x...
     * Response: { "states": [ ChainStateDTO, ... ] }
     */
    public static List<ChainStateDTO> fetchAllChainStates(String userAddress) throws Exception {
        String body = doGet("/games/chain-states?user_address=" + userAddress);
        JSONObject json = new JSONObject(body);
        JSONArray arr = json.getJSONArray("states");
        Type listType = new TypeToken<List<ChainStateDTO>>(){}.getType();
        return gson.fromJson(arr.toString(), listType);
    }

    /**
     * 同步链上状态到后端 DB（交易确认后调用）
     *
     * POST /api/v1/gold/games/{gameId}/chain-state/sync
     * Body: ChainStateSyncReq
     */
    public static boolean syncChainState(int gameId, ChainStateSyncReq req) throws Exception {
        String body = doPost("/games/" + gameId + "/chain-state/sync", gson.toJson(req));
        JSONObject json = new JSONObject(body);
        return json.optBoolean("success", false);
    }

    // ==================== 历史价格数据 API ====================

    /**
     * 获取游戏的历史价格数据（用于折线图显示）
     *
     * GET /api/v1/gold/games/{gameId}/history
     * Response: { "history": [ HistoryPointDTO, ... ] }
     */
    public static List<HistoryPointDTO> fetchHistory(int gameId) throws Exception {
        String body = doGet("/games/" + gameId + "/history");
        JSONObject json = new JSONObject(body);
        JSONArray arr = json.getJSONArray("history");
        Type listType = new TypeToken<List<HistoryPointDTO>>(){}.getType();
        return gson.fromJson(arr.toString(), listType);
    }

    /**
     * 添加一条历史价格记录（交易完成后调用）
     *
     * POST /api/v1/gold/games/{gameId}/history
     * Body: HistoryPointDTO
     */
    public static boolean addHistoryPoint(int gameId, HistoryPointDTO point) throws Exception {
        String body = doPost("/games/" + gameId + "/history", gson.toJson(point));
        JSONObject json = new JSONObject(body);
        return json.optBoolean("success", false);
    }

    // ==================== 交易同步 API ====================

    /**
     * 交易完成后同步状态到后端 DB
     * 调用时机：buyShares / sellShares / claimReward 交易确认后
     *
     * POST /api/v1/gold/trades/sync
     * Body: TradeSyncReq
     */
    public static boolean syncTrade(TradeSyncReq req) throws Exception {
        String body = doPost("/trades/sync", gson.toJson(req));
        JSONObject json = new JSONObject(body);
        return json.optBoolean("success", false);
    }

    // ==================== AI 托管状态 API ====================

    /**
     * 查询 AI 托管状态
     *
     * GET /api/v1/gold/ai-managed?game_id=X&user_address=Y&contract_address=Z
     */
    public static boolean getAiManagedStatus(int gameId, String userAddress, String contractAddress) throws Exception {
        String path = String.format("/ai-managed?game_id=%d&user_address=%s&contract_address=%s",
                gameId, userAddress, contractAddress);
        String body = doGet(path);
        JSONObject json = new JSONObject(body);
        return json.optBoolean("enabled", false);
    }

    /**
     * 设置 AI 托管状态
     *
     * POST /api/v1/gold/ai-managed
     * Body: { game_id, user_address, enabled, contract_address, private_key }
     */
    public static boolean setAiManagedStatus(int gameId, String userAddress, boolean enabled,
                                              String contractAddress, String privateKey) throws Exception {
        JSONObject json = new JSONObject();
        json.put("game_id", gameId);
        json.put("user_address", userAddress);
        json.put("enabled", enabled);
        json.put("contract_address", contractAddress);
        json.put("private_key", privateKey);
        String body = doPost("/ai-managed", json.toString());
        JSONObject resp = new JSONObject(body);
        return resp.optBoolean("success", false);
    }

    // ==================== DTO 定义 ====================

    /**
     * 游戏元数据 DTO（对应后端 DB 的 gold_games 表）
     */
    public static class GameMetaDTO {
        @SerializedName("game_id")
        public int gameId;

        @SerializedName("contract_address")
        public String contractAddress;

        @SerializedName("ipfs_cid")
        public String ipfsCid;

        @SerializedName("desc")
        public String desc;

        @SerializedName("condition")
        public String condition;

        @SerializedName("avatar_url")
        public String avatarUrl;

        @SerializedName("detailed_info")
        public String detailedInfo;

        @SerializedName("option_yes")
        public String optionYES;

        @SerializedName("option_no")
        public String optionNO;

        @SerializedName("creator_address")
        public String creatorAddress;

        @SerializedName("created_at")
        public String createdAt;
    }

    /**
     * 同步游戏元数据请求
     */
    public static class GameMetaSyncReq {
        @SerializedName("game_id")
        public int gameId;

        @SerializedName("contract_address")
        public String contractAddress;

        @SerializedName("ipfs_cid")
        public String ipfsCid;

        @SerializedName("desc")
        public String desc;

        @SerializedName("condition")
        public String condition;

        @SerializedName("avatar_url")
        public String avatarUrl;

        @SerializedName("detailed_info")
        public String detailedInfo;

        @SerializedName("option_yes")
        public String optionYES;

        @SerializedName("option_no")
        public String optionNO;

        @SerializedName("creator_address")
        public String creatorAddress;

        @SerializedName("duration_sec")
        public long durationSec;

        @SerializedName("initial_liquidity_wei")
        public String initialLiquidityWei;
    }

    /**
     * 链上状态缓存 DTO（由后端定时从链上同步）
     */
    public static class ChainStateDTO {
        @SerializedName("game_id")
        public int gameId;

        @SerializedName("total_pool")
        public String totalPool;

        @SerializedName("is_resolved")
        public boolean isResolved;

        @SerializedName("is_refunded")
        public boolean isRefunded;

        @SerializedName("winning_option")
        public int winningOption;

        @SerializedName("deadline_sec")
        public long deadlineSec;

        @SerializedName("reserve_yes")
        public String reserveYES;

        @SerializedName("reserve_no")
        public String reserveNO;

        @SerializedName("my_shares_yes")
        public String mySharesYES;

        @SerializedName("my_shares_no")
        public String mySharesNO;

        @SerializedName("updated_at")
        public String updatedAt;
    }

    /**
     * 链上状态同步请求
     */
    public static class ChainStateSyncReq {
        @SerializedName("total_pool")
        public String totalPool;

        @SerializedName("is_resolved")
        public boolean isResolved;

        @SerializedName("is_refunded")
        public boolean isRefunded;

        @SerializedName("winning_option")
        public int winningOption;

        @SerializedName("reserve_yes")
        public String reserveYES;

        @SerializedName("reserve_no")
        public String reserveNO;

        @SerializedName("my_shares_yes")
        public String mySharesYES;

        @SerializedName("my_shares_no")
        public String mySharesNO;
    }

    /**
     * 历史价格点 DTO
     */
    public static class HistoryPointDTO {
        @SerializedName("game_id")
        public int gameId;

        @SerializedName("timestamp_sec")
        public long timestampSec;

        @SerializedName("yes_price")
        public float yesPrice;

        @SerializedName("no_price")
        public float noPrice;

        @SerializedName("total_pool")
        public String totalPool;
    }

    /**
     * 交易同步请求
     */
    public static class TradeSyncReq {
        @SerializedName("game_id")
        public int gameId;

        @SerializedName("contract_address")
        public String contractAddress;

        @SerializedName("user_address")
        public String userAddress;

        @SerializedName("trade_type")
        public String tradeType; // "BUY", "SELL", "CLAIM"

        @SerializedName("option_id")
        public int optionId;

        @SerializedName("amount_wei")
        public String amountWei;

        @SerializedName("tx_hash")
        public String txHash;

        @SerializedName("is_success")
        public boolean isSuccess;

        // 同步当前链上状态
        @SerializedName("total_pool_after")
        public String totalPoolAfter;

        @SerializedName("reserve_yes_after")
        public String reserveYESAfter;

        @SerializedName("reserve_no_after")
        public String reserveNOAfter;

        @SerializedName("my_shares_yes_after")
        public String mySharesYESAfter;

        @SerializedName("my_shares_no_after")
        public String mySharesNOAfter;
    }
}
