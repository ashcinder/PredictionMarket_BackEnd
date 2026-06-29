package com.example.brokerfi.xc.agent.gold.model.data;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import com.example.brokerfi.BuildConfig;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldMarketSecurityPolicy;

import org.json.JSONObject;
import org.web3j.abi.FunctionEncoder;
import org.web3j.abi.FunctionReturnDecoder;
import org.web3j.abi.TypeReference;
import org.web3j.abi.datatypes.*;
import org.web3j.abi.datatypes.generated.*;
import org.web3j.crypto.Credentials;
import org.web3j.protocol.Web3j;
import org.web3j.protocol.core.DefaultBlockParameterName;
import org.web3j.protocol.core.methods.request.Transaction;
import org.web3j.protocol.core.methods.response.EthCall;
import org.web3j.protocol.core.methods.response.EthGetTransactionReceipt;
import org.web3j.protocol.core.methods.response.EthSendTransaction;
import org.web3j.protocol.core.methods.response.TransactionReceipt;
import org.web3j.protocol.http.HttpService;

import java.math.BigDecimal;
import java.math.BigInteger;
import java.net.URL;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

/**
 * GoldMarketRepository - 黄金预测市场数据仓库
 *
 * ==================== 三层数据交互架构 ====================
 *
 * 【读操作】优先级：后端 DB → 链上/IPFS（回退）
 *   1. 优先从 Go 后端 DB 读取元数据和缓存的链上状态（低延迟）
 *   2. 后端不可用时，回退到链上 eth_call + IPFS 直读（高延迟但可靠）
 *
 * 【写操作】同步写入：IPFS → 链上交易 → 查询真实状态 → 后端 DB（onConfirmed 前完成）
 *   1. 上传图片/元数据到 IPFS（去中心化存储）
 *   2. 发送交易到链上，等待确认（不可篡改的状态变更）
 *   3. 通过 eth_call 查询交易后的链上真实状态（资金池、储备金、持仓）
 *   4. 将真实状态同步写入后端 DB（加速后续读取，确保缓存一致性）
 *   5. 后端 DB 写入完成后，回调 onConfirmed 通知 UI
 *   注意：步骤 4 失败不阻塞主流程（非关键路径），每项同步独立 try-catch
 *
 * ==================== 三层职责划分 ====================
 * - IPFS：存储图片、元数据 JSON（去中心化、不可篡改的内容寻址存储）
 * - 区块链：存储合约状态（资金、份额、结算结果，不可篡改的账本）
 * - 后端 DB：缓存元数据和链上状态、存储历史价格、用户 AI 托管配置（快速查询）
 */
public class GoldMarketRepository {
    private static final String TAG = "GoldMarketRepo";

    private static final String PREFS_NAME = "gold_market_prefs";
    private static final String KEY_CONTRACT_ADDR = "contract_address";
    private static final String KEY_CONTRACT_ADDRS = "contract_addresses";
    private static final String KEY_RPC_URL = "rpc_url";
    private static final BigDecimal WEI_PER_BKC = new BigDecimal("1000000000000000000");
    private static final BigInteger LOCAL_RPC_CALL_GAS_LIMIT = new BigInteger("5000000");
    private static final BigInteger LOCAL_RPC_CALL_GAS_PRICE = BigInteger.ZERO;
    private static final BigInteger LOCAL_RPC_CALL_VALUE = BigInteger.ZERO;
    public static final int GOLD_GAME_ID = 1;

    private static List<String> cachedAddresses;

    private final String privateKey;
    private final String contractAddress;
    private final boolean useLocalRpc;
    private final Web3j web3j;
    private final Credentials credentials;
    private final String walletAddress;
    private final boolean developerMarketToolsEnabled;

    // ── config ──

    public static String getContractAddress(Context ctx) {
        return getContractAddresses(ctx).get(0);
    }

    public static List<String> getContractAddresses(Context ctx) {
        boolean developerToolsEnabled = GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG);
        if (cachedAddresses != null && developerToolsEnabled) return new ArrayList<>(cachedAddresses);
        SharedPreferences prefs = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        String saved = prefs.getString(KEY_CONTRACT_ADDRS, null);
        if (saved == null || saved.trim().isEmpty()) {
            saved = prefs.getString(KEY_CONTRACT_ADDR, null);
        }
        cachedAddresses = GoldMarketSecurityPolicy.resolveContractAddresses(developerToolsEnabled, saved);
        return new ArrayList<>(cachedAddresses);
    }

    static List<String> parseContractAddresses(String rawAddresses) {
        return GoldMarketSecurityPolicy.parseContractAddresses(rawAddresses);
    }

    public static void setContractAddress(Context ctx, String address) {
        if (!GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG)) return;
        setContractAddresses(ctx, Collections.singletonList(address));
    }

    public static void setContractAddresses(Context ctx, List<String> addresses) {
        if (!GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG)) return;
        StringBuilder joined = new StringBuilder();
        List<String> validAddresses = new ArrayList<>();
        for (String address : addresses) {
            if (!GoldMarketSecurityPolicy.isValidContractAddress(address)) continue;
            validAddresses.add(address.trim());
            if (joined.length() > 0) joined.append('\n');
            joined.append(address.trim());
        }
        ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit()
                .putString(KEY_CONTRACT_ADDRS, joined.toString())
                .putString(KEY_CONTRACT_ADDR, validAddresses.isEmpty() ? "" : validAddresses.get(0))
                .apply();
        cachedAddresses = GoldMarketSecurityPolicy.resolveContractAddresses(true, joined.toString());
    }

    public static String getRpcUrl(Context ctx) {
        String saved = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getString(KEY_RPC_URL, "");
        return GoldMarketSecurityPolicy.resolveRpcUrl(
                GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG), saved);
    }

    public static void setRpcUrl(Context ctx, String url) {
        if (!GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG)) return;
        ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putString(KEY_RPC_URL, url).apply();
    }

    // ── constructor ──

    public GoldMarketRepository(Context ctx, String privateKey) {
        this(ctx, privateKey, getContractAddress(ctx));
    }

    public GoldMarketRepository(Context ctx, String privateKey, String contractAddress) {
        this.privateKey = privateKey;
        this.developerMarketToolsEnabled = GoldMarketSecurityPolicy.isDeveloperMarketToolsEnabled(BuildConfig.DEBUG);
        this.contractAddress = contractAddress == null ? getContractAddress(ctx) : contractAddress.trim();
        String rpcUrl = getRpcUrl(ctx);
        this.useLocalRpc = rpcUrl != null && !rpcUrl.isEmpty();

        // 初始化 BackendApiClient 的 Base URL（根据 DEBUG 自动选择本地/生产地址）
        BackendApiClient.getBaseUrl(ctx);

        if (useLocalRpc) {
            Log.d(TAG, "LocalRPC mode: url=" + rpcUrl + " wallet=" + Credentials.create(privateKey).getAddress());
            this.web3j = Web3j.build(new HttpService(rpcUrl));
            this.credentials = Credentials.create(privateKey);
            this.walletAddress = credentials.getAddress();
        } else {
            this.web3j = null;
            this.credentials = null;
            this.walletAddress = BrokerChainClient.getAddress(privateKey);
        }
    }

    public String getWalletAddress() {
        return walletAddress != null ? walletAddress : "";
    }

    public String getBoundContractAddress() {
        return contractAddress;
    }

    // ── callbacks ──

    public interface DataCallback<T> {
        void onSuccess(T result);
        void onError(String error);
        /** 调试用：报告数据来源和耗时，默认空实现不影响已有代码 */
        default void onTiming(String source, long durationMs, boolean isFallback) {}
    }

    public interface TxCallback {
        void onTxSent(String txHash);
        void onConfirmed(String message);
        void onError(String error);
    }

    public static BigInteger parseTokenAmountToWei(String amountText) {
        if (amountText == null) return null;
        String normalized = amountText.trim();
        if (normalized.isEmpty()) return null;
        try {
            BigDecimal amount = new BigDecimal(normalized);
            if (amount.compareTo(BigDecimal.ZERO) <= 0) return null;
            BigInteger wei = amount.multiply(WEI_PER_BKC).toBigIntegerExact();
            return wei.compareTo(BigInteger.ZERO) > 0 ? wei : null;
        } catch (ArithmeticException | NumberFormatException e) {
            return null;
        }
    }

    /**
     * 构建 claimReward 合约调用（optionId 使用 UI 约定 0=YES, 1=NO，内部转换为合约约定）
     */
    public static org.web3j.abi.datatypes.Function buildClaimRewardFunction(int gameId, int uiOptionId) {
        // UI (0=YES, 1=NO) 与合约约定一致 (0=YES, 1=NO)
        return new org.web3j.abi.datatypes.Function(
            "claimReward",
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(uiOptionId))),
            Collections.emptyList());
    }

    public static org.web3j.abi.datatypes.Function buildGameCountFunction() {
        return new org.web3j.abi.datatypes.Function(
            "gameCount",
            Collections.emptyList(),
            Collections.singletonList(new TypeReference<Uint256>() {}));
    }

    // ── RPC transport ──

    static Transaction buildLocalEthCallTransaction(String from, String to, String data) {
        return Transaction.createFunctionCallTransaction(
                from, null, LOCAL_RPC_CALL_GAS_PRICE, LOCAL_RPC_CALL_GAS_LIMIT,
                to, LOCAL_RPC_CALL_VALUE, data);
    }

    static Transaction buildLocalWriteTransaction(String from, String to, String data, BigInteger value) {
        return Transaction.createFunctionCallTransaction(
                from, null, LOCAL_RPC_CALL_GAS_PRICE, LOCAL_RPC_CALL_GAS_LIMIT,
                to, value, data);
    }

    private String ethCall(org.web3j.abi.datatypes.Function function) throws Exception {
        String data = FunctionEncoder.encode(function);
        if (useLocalRpc) {
            Log.d(TAG, "standard ethCall to=" + contractAddress + " data=" + data.substring(0, Math.min(66, data.length())) + "...");
            Transaction txn = buildLocalEthCallTransaction(walletAddress, contractAddress, data);
            EthCall resp = web3j.ethCall(txn, DefaultBlockParameterName.LATEST).send();
            Log.d(TAG, "standard ethCall result: hasError=" + resp.hasError() + " value=" + resp.getValue());
            if (resp.hasError()) throw new Exception(resp.getError().getMessage());
            String value = resp.getValue();
            if (value == null || value.isEmpty()) {
                throw new Exception("BrokerChain local RPC returned empty eth_call result; check call fields and game id");
            }
            return value;
        } else {
            String response = BrokerChainClient.sendEthCall(privateKey, contractAddress, data);
            Log.d(TAG, "ethCall response: " + (response != null ? response.substring(0, Math.min(200, response.length())) : "null"));
            return extractHexResult(response);
        }
    }

    private void sendTransaction(BigInteger value, org.web3j.abi.datatypes.Function function,
                                 String successMsg, TxCallback callback) {
        sendTransaction(value, function, successMsg, callback, null);
    }

    /**
     * 发送链上交易，交易确认后【先同步后端 DB，再通知 UI】
     *
     * 完整流程（三步串行）：
     * 1. 发送交易并等待链上确认（本地 RPC 等待 receipt，BrokerChain 等待固定延迟）
     * 2. 通过 eth_call 查询交易后的链上真实状态（资金池、储备金、持仓、结算状态）
     * 3. 将三项数据同步写入后端 DB（交易记录 + 历史价格点 + 链上状态缓存）
     * 4. 后端 DB 写入完成后，回调 onConfirmed 通知 UI
     *
     * 设计意图：确保 onConfirmed 回调时，后端 DB 已包含最新的交易数据，
     * UI 可以立即从后端 DB 拉取到正确的状态，无需额外等待。
     */
    private void sendTransaction(BigInteger value, org.web3j.abi.datatypes.Function function,
                                 String successMsg, TxCallback callback, TradeSyncInfo tradeInfo) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                String data = FunctionEncoder.encode(function);
                if (useLocalRpc) {
                    // ── Local RPC 模式 ──
                    String txHash = sendLocalRpcAndWait(value, data, callback);
                    if (txHash == null) return; // 错误已在 sendLocalRpcAndWait 中回调
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onTxSent(txHash));

                    // 查询交易后的链上真实状态
                    PostTxState postState = (tradeInfo != null) ? queryPostTxState(tradeInfo.gameId) : null;

                    // 同步到后端 DB（在 onConfirmed 之前，确保数据一致性）
                    if (tradeInfo != null) {
                        syncAllToBackend(tradeInfo, txHash, postState);
                    }

                    AppExecutors.getInstance().mainThread().execute(() -> callback.onConfirmed(successMsg));

                } else {
                    // ── BrokerChain 模式 ──
                    String response = sendBrokerChainTx(value, data, callback);
                    if (response == null) return; // 错误已在 sendBrokerChainTx 中回调
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onTxSent("Transaction Sent"));

                    // 等待链上确认（BrokerChain 出块时间 ~3 秒，留余量等待 8 秒）
                    Thread.sleep(8000);
                    try {
                        BrokerChainClient.ReturnAccountState state = BrokerChainClient.getAddrAndBalance(privateKey);
                        Log.d(TAG, "BrokerChain 确认后余额: " + (state != null ? state.getBalance() : "null"));
                    } catch (Exception balanceErr) {
                        Log.w(TAG, "BrokerChain 余额查询异常: " + balanceErr.getMessage());
                    }

                    // 查询交易后的链上真实状态
                    PostTxState postState = (tradeInfo != null) ? queryPostTxState(tradeInfo.gameId) : null;

                    // 同步到后端 DB（在 onConfirmed 之前）
                    if (tradeInfo != null) {
                        syncAllToBackend(tradeInfo, response, postState);
                    }

                    AppExecutors.getInstance().mainThread().execute(() -> callback.onConfirmed(successMsg));
                }
            } catch (Exception e) {
                postError(callback, "交易异常: " + e.getMessage());
            }
        });
    }

    /**
     * Local RPC 模式：发送交易并等待链上确认（receipt）
     * @return txHash，失败时回调 onError 并返回 null
     */
    private String sendLocalRpcAndWait(BigInteger value, String data, TxCallback callback) throws Exception {
        Transaction txn = buildLocalWriteTransaction(walletAddress, contractAddress, data, value);
        Log.d(TAG, "standard eth_sendTransaction to=" + contractAddress
                + " value=" + txn.getValue()
                + " data=" + data.substring(0, Math.min(66, data.length())) + "...");
        EthSendTransaction resp = web3j.ethSendTransaction(txn).send();
        if (resp.hasError()) {
            postError(callback, resp.getError().getMessage());
            return null;
        } else if (resp.getTransactionHash() == null || resp.getTransactionHash().isEmpty()) {
            postError(callback, "本地 RPC 未返回交易哈希，交易未确认提交");
            return null;
        }
        String txHash = resp.getTransactionHash();
        Log.d(TAG, "standard eth_sendTransaction hash=" + txHash);
        waitForLocalReceipt(txHash);
        return txHash;
    }

    /**
     * BrokerChain 模式：发送交易到远程服务器
     * @return 服务器返回的响应字符串，失败时回调 onError 并返回 null
     */
    private String sendBrokerChainTx(BigInteger value, String data, TxCallback callback) throws Exception {
        String valueHex = value.compareTo(BigInteger.ZERO) > 0 ? value.toString(16) : "0x0";
        Log.d(TAG, "brokerChainSendTx: to=" + contractAddress
                + " value=" + valueHex
                + " data=" + data.substring(0, Math.min(66, data.length())) + "...");
        String response = BrokerChainClient.sendEthTx(privateKey, contractAddress, data, valueHex);
        Log.d(TAG, "brokerChainSendTx response: " + (response != null ? response.substring(0, Math.min(200, response.length())) : "null"));

        if (response == null || response.toLowerCase().contains("error") || response.toLowerCase().contains("failed")) {
            postError(callback, "交易失败: " + response);
            return null;
        }
        return response;
    }

    // ── 交易后链上状态查询 ──

    /**
     * 交易后的链上真实状态快照（用于同步到后端 DB）
     */
    private static class PostTxState {
        String totalPool;
        boolean isResolved;
        boolean isRefunded;
        int winningOption;
        long deadlineSec;       // 链上绝对截止时间戳（秒），同步到后端 DB 避免显示"已截止"
        String reserveYES;
        String reserveNO;
        String mySharesYES;
        String mySharesNO;
    }

    /**
     * 查询交易后的链上真实状态
     * 通过 eth_call 调用 getGameInfo + getGameExtraData 合约方法，
     * 获取资金池、储备金、用户持仓、结算状态等真实值。
     *
     * @param gameId 博弈池 ID
     * @return PostTxState，查询失败返回 null
     */
    private PostTxState queryPostTxState(int gameId) {
        try {
            org.web3j.abi.datatypes.Function fInfo = new org.web3j.abi.datatypes.Function(
                "getGameInfo", Collections.singletonList(new Uint256(BigInteger.valueOf(gameId))),
                Arrays.asList(
                    new TypeReference<Utf8String>() {},   // ipfsCID
                    new TypeReference<Uint256>() {},      // totalPool
                    new TypeReference<Bool>() {},         // isResolved
                    new TypeReference<Uint8>() {},        // winningOption
                    new TypeReference<Uint256>() {},      // deadlineSec
                    new TypeReference<Bool>() {}          // isRefunded
                ));

            String addr = getWalletAddress();
            if (addr == null || addr.isEmpty()) addr = "0x0000000000000000000000000000000000000000";
            org.web3j.abi.datatypes.Function fExtra = new org.web3j.abi.datatypes.Function(
                "getGameExtraData",
                Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Address(addr)),
                Arrays.asList(
                    new TypeReference<DynamicArray<Uint256>>() {},
                    new TypeReference<DynamicArray<Uint256>>() {}
                ));

            String hexInfo = ethCall(fInfo);
            String hexExtra = ethCall(fExtra);

            if (hexInfo == null || hexInfo.equals("0x")) {
                Log.w(TAG, "queryPostTxState: getGameInfo 返回空, gameId=" + gameId);
                return null;
            }

            List<Type> res = FunctionReturnDecoder.decode(hexInfo, fInfo.getOutputParameters());
            if (res.size() < 6) return null;

            PostTxState state = new PostTxState();
            state.totalPool = ((Uint256) res.get(1)).getValue().toString();
            state.isResolved = ((Bool) res.get(2)).getValue();
            // 合约约定与 UI 一致 (0=YES, 1=NO)
            state.winningOption = ((Uint8) res.get(3)).getValue().intValue();
            state.deadlineSec = ((Uint256) res.get(4)).getValue().longValue();  // 链上绝对截止时间戳（秒）
            state.isRefunded = ((Bool) res.get(5)).getValue();

            if (hexExtra != null && !hexExtra.equals("0x")) {
                try {
                    List<Type> extraRes = FunctionReturnDecoder.decode(hexExtra, fExtra.getOutputParameters());
                    if (extraRes.size() >= 2) {
                        List<Uint256> reservesArray = ((DynamicArray<Uint256>) extraRes.get(0)).getValue();
                        List<Uint256> sharesArray = ((DynamicArray<Uint256>) extraRes.get(1)).getValue();
                        // 合约返回顺序: [reserveNO, reserveYES], [mySharesYES, mySharesNO]
                        if (reservesArray.size() >= 2) {
                            state.reserveYES = reservesArray.get(1).getValue().toString();
                            state.reserveNO = reservesArray.get(0).getValue().toString();
                        }
                        if (sharesArray.size() >= 2) {
                            state.mySharesYES = sharesArray.get(0).getValue().toString();
                            state.mySharesNO = sharesArray.get(1).getValue().toString();
                        }
                    }
                } catch (Exception e) {
                    Log.w(TAG, "queryPostTxState: getGameExtraData 解析失败 - " + e.getMessage());
                }
            }

            Log.d(TAG, "queryPostTxState 成功: gameId=" + gameId
                    + " totalPool=" + state.totalPool
                    + " isResolved=" + state.isResolved
                    + " deadlineSec=" + state.deadlineSec
                    + " mySharesYES=" + state.mySharesYES);
            return state;
        } catch (Exception e) {
            Log.w(TAG, "queryPostTxState 异常: gameId=" + gameId + " - " + e.getMessage());
            return null;
        }
    }

    // ── 后端 DB 同步（同步执行，在 onConfirmed 之前调用） ──

    /**
     * 将交易数据同步写入后端 DB（同步执行，在 onConfirmed 之前调用）
     *
     * 写入策略（三项同步，每项独立 try-catch）：
     * 1. 同步交易记录 → POST /api/v1/gold/trades/sync
     * 2. 添加历史价格点 → POST /api/v1/gold/games/{gameId}/history
     * 3. 同步链上状态缓存 → POST /api/v1/gold/games/{gameId}/chain-state/sync
     *
     * 优先使用 postState（链上查询的真实值），
     * postState 为 null 时回退使用 tradeInfo 中的值。
     * 每项同步失败独立处理，不阻塞其他同步。
     */
    private void syncAllToBackend(TradeSyncInfo tradeInfo, String txHash, PostTxState postState) {
        final int gameId = tradeInfo.gameId;

        // ── 1. 同步交易记录 ──
        try {
            BackendApiClient.TradeSyncReq tradeReq = new BackendApiClient.TradeSyncReq();
            tradeReq.gameId = gameId;
            tradeReq.contractAddress = contractAddress;
            tradeReq.userAddress = walletAddress;
            tradeReq.tradeType = tradeInfo.tradeType;
            tradeReq.optionId = tradeInfo.optionId;
            tradeReq.amountWei = tradeInfo.amountWei;
            tradeReq.txHash = txHash;
            tradeReq.isSuccess = true;
            if (postState != null) {
                tradeReq.totalPoolAfter = postState.totalPool;
                tradeReq.reserveYESAfter = postState.reserveYES;
                tradeReq.reserveNOAfter = postState.reserveNO;
                tradeReq.mySharesYESAfter = postState.mySharesYES;
                tradeReq.mySharesNOAfter = postState.mySharesNO;
            } else {
                tradeReq.totalPoolAfter = tradeInfo.totalPoolAfter;
                tradeReq.reserveYESAfter = tradeInfo.reserveYESAfter;
                tradeReq.reserveNOAfter = tradeInfo.reserveNOAfter;
                tradeReq.mySharesYESAfter = tradeInfo.mySharesYESAfter;
                tradeReq.mySharesNOAfter = tradeInfo.mySharesNOAfter;
            }
            BackendApiClient.syncTrade(tradeReq);
            Log.d(TAG, "✅ 后端交易同步成功: gameId=" + gameId + " type=" + tradeInfo.tradeType);
        } catch (Exception e) {
            Log.w(TAG, "❌ 后端交易同步失败（非关键）: gameId=" + gameId + " - " + e.getMessage());
        }

        // ── 2. 添加历史价格点 ──
        try {
            String reserveYES = (postState != null) ? postState.reserveYES : tradeInfo.reserveYESAfter;
            String reserveNO = (postState != null) ? postState.reserveNO : tradeInfo.reserveNOAfter;
            String totalPool = (postState != null) ? postState.totalPool : tradeInfo.totalPoolAfter;

            if (reserveYES != null && reserveNO != null) {
                BackendApiClient.HistoryPointDTO point = new BackendApiClient.HistoryPointDTO();
                point.gameId = gameId;
                point.timestampSec = System.currentTimeMillis() / 1000;
                BigInteger yesRes = new BigInteger(reserveYES);
                BigInteger noRes = new BigInteger(reserveNO);
                BigInteger total = yesRes.add(noRes);
                if (total.compareTo(BigInteger.ZERO) > 0) {
                    point.yesPrice = (float) (yesRes.doubleValue() / total.doubleValue() * 100);
                    point.noPrice = 100 - point.yesPrice;
                }
                point.totalPool = totalPool;
                BackendApiClient.addHistoryPoint(gameId, point);
                Log.d(TAG, "✅ 后端历史数据同步成功: gameId=" + gameId);
            }
        } catch (Exception e) {
            Log.w(TAG, "❌ 后端历史数据同步失败（非关键）: gameId=" + gameId + " - " + e.getMessage());
        }

        // ── 3. 同步链上状态缓存 ──
        try {
            BackendApiClient.ChainStateSyncReq chainReq = new BackendApiClient.ChainStateSyncReq();
            if (postState != null) {
                chainReq.totalPool = postState.totalPool;
                chainReq.isResolved = postState.isResolved;
                chainReq.isRefunded = postState.isRefunded;
                chainReq.winningOption = postState.winningOption;
                chainReq.deadlineSec = postState.deadlineSec;
                chainReq.reserveYES = postState.reserveYES;
                chainReq.reserveNO = postState.reserveNO;
                chainReq.mySharesYES = postState.mySharesYES;
                chainReq.mySharesNO = postState.mySharesNO;
            } else {
                chainReq.totalPool = tradeInfo.totalPoolAfter;
                chainReq.isResolved = tradeInfo.isResolved;
                chainReq.isRefunded = tradeInfo.isRefunded;
                chainReq.winningOption = tradeInfo.winningOption;
                chainReq.deadlineSec = tradeInfo.deadlineSec;
                chainReq.reserveYES = tradeInfo.reserveYESAfter;
                chainReq.reserveNO = tradeInfo.reserveNOAfter;
                chainReq.mySharesYES = tradeInfo.mySharesYESAfter;
                chainReq.mySharesNO = tradeInfo.mySharesNOAfter;
            }
            BackendApiClient.syncChainState(gameId, chainReq);
            Log.d(TAG, "✅ 后端链上状态同步成功: gameId=" + gameId
                    + " totalPool=" + chainReq.totalPool
                    + " isResolved=" + chainReq.isResolved
                    + " deadlineSec=" + chainReq.deadlineSec);
        } catch (Exception e) {
            Log.w(TAG, "❌ 后端链上状态同步失败（非关键）: gameId=" + gameId + " - " + e.getMessage());
        }
    }

    private void waitForLocalReceipt(String txHash) throws Exception {
        for (int i = 0; i < 12; i++) {
            Thread.sleep(2500);
            EthGetTransactionReceipt receiptResp = web3j.ethGetTransactionReceipt(txHash).send();
            if (receiptResp.hasError()) {
                throw new Exception(receiptResp.getError().getMessage());
            }
            Optional<TransactionReceipt> receipt = receiptResp.getTransactionReceipt();
            if (receipt.isPresent()) {
                String status = receipt.get().getStatus();
                if ("0x0".equals(status)) {
                    throw new Exception("交易执行失败，链上 receipt status=0x0");
                }
                Log.d(TAG, "local tx confirmed: " + txHash + " status=" + status);
                return;
            }
        }
        throw new Exception("交易已提交但 30 秒内未确认，请稍后手动刷新");
    }

    private String extractHexResult(String responseJson) {
        if (responseJson == null || responseJson.isEmpty()) { Log.w(TAG, "extractHexResult: null/empty"); return "0x"; }
        try {
            if (responseJson.trim().startsWith("{")) {
                JSONObject obj = new JSONObject(responseJson);
                String res = "0x";
                if (obj.has("result")) res = obj.getString("result");
                else if (obj.has("data")) res = obj.getString("data");
                else Log.w(TAG, "extractHexResult: no result/data. Keys: " + obj.keys());
                if (res.toLowerCase().contains("reverted") || res.toLowerCase().contains("error")) return "0x";
                return res;
            }
            if (responseJson.toLowerCase().contains("reverted")) return "0x";
            return responseJson.trim();
        } catch (Exception e) { return "0x"; }
    }

    // ── 辅助：从 BackendApiClient DTO 构建 GameModel ──

    private GameModel buildModelFromBackend(BackendApiClient.GameMetaDTO meta, BackendApiClient.ChainStateDTO state) {
        GameModel m = new GameModel();
        m.id = meta.gameId;
        m.contractAddress = meta.contractAddress;
        m.ipfsCID = meta.ipfsCid;
        m.desc = meta.desc != null && !meta.desc.isEmpty() ? meta.desc : ("博弈池 #" + meta.gameId);
        m.condition = meta.condition != null ? meta.condition : "";
        m.avatarUrl = meta.avatarUrl != null ? meta.avatarUrl : "";
        m.detailedInfo = meta.detailedInfo != null ? meta.detailedInfo : "";
        m.optionNames = Arrays.asList(
                meta.optionYES != null ? meta.optionYES : "YES",
                meta.optionNO != null ? meta.optionNO : "NO");
        m.optionCount = 2;

        if (state != null) {
            m.totalPool = parseBigInteger(state.totalPool);
            m.isResolved = state.isResolved;
            m.isRefunded = state.isRefunded;
            // 合约约定与 UI 一致 (0=YES, 1=NO)
            m.winningOption = toUiOption(state.winningOption);
            m.deadlineSec = state.deadlineSec;
            BigInteger resYES = parseBigInteger(state.reserveYES);
            BigInteger resNO = parseBigInteger(state.reserveNO);
            // 核心修复：Java 索引 0 必须为 reserveNO，以匹配 UI 概率计算 (res0 / total)
            m.virtualReserves = Arrays.asList(resNO, resYES);
            BigInteger myYES = parseBigInteger(state.mySharesYES);
            BigInteger myNo = parseBigInteger(state.mySharesNO);
            m.myShares = Arrays.asList(myYES, myNo);
        } else {
            m.totalPool = BigInteger.ZERO;
            m.virtualReserves = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
            m.myShares = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
        }

        // 尝试从后端获取历史数据
        try {
            List<BackendApiClient.HistoryPointDTO> backendHistory = BackendApiClient.fetchHistory(meta.gameId);
            if (backendHistory != null && !backendHistory.isEmpty()) {
                m.history = new ArrayList<>();
                for (BackendApiClient.HistoryPointDTO p : backendHistory) {
                    HistoryPoint hp = new HistoryPoint();
                    hp.time = p.timestampSec;
                    hp.yesPrice = p.yesPrice;
                    hp.noPrice = p.noPrice;
                    m.history.add(hp);
                }
            } else {
                m.history = generateMockHistory(m.virtualReserves);
            }
        } catch (Exception e) {
            m.history = generateMockHistory(m.virtualReserves);
        }

        return m;
    }

    private BigInteger parseBigInteger(String s) {
        if (s == null || s.isEmpty()) return BigInteger.ZERO;
        try { return new BigInteger(s); }
        catch (NumberFormatException e) { return BigInteger.ZERO; }
    }

    // ── 辅助：从 IPFS 补充元数据 ──

    private void enrichFromIPFS(GameModel model) {
        if (model.ipfsCID == null || model.ipfsCID.isEmpty()) {
            model.desc = "博弈池 #" + model.id;
            model.history = generateMockHistory(model.virtualReserves);
            return;
        }
        try {
            String json = PinataClient.downloadJsonFromIPFS(model.ipfsCID);
            if (json != null && !json.isEmpty()) {
                JSONObject obj = new JSONObject(json);
                model.desc = obj.optString("desc", "博弈池 #" + model.id);
                model.condition = obj.optString("condition", "");
                model.avatarUrl = obj.optString("avatarUrl", "");
                model.detailedInfo = obj.optString("detailedInfo", "");
                model.optionNames = Arrays.asList(
                        obj.optString("optionYES", "YES"),
                        obj.optString("optionNO", "NO"));
                model.optionCount = 2;

                if (obj.has("history")) {
                    org.json.JSONArray historyArr = obj.getJSONArray("history");
                    model.history = new ArrayList<>();
                    for (int i = 0; i < historyArr.length(); i++) {
                        JSONObject point = historyArr.getJSONObject(i);
                        HistoryPoint hp = new HistoryPoint();
                        hp.time = point.optLong("t", point.optLong("time", 0));
                        hp.yesPrice = (float) point.optDouble("y", point.optDouble("yes", 0));
                        hp.noPrice = (float) point.optDouble("n", point.optDouble("no", 0));
                        model.history.add(hp);
                    }
                } else {
                    model.history = generateMockHistory(model.virtualReserves);
                }
            } else {
                model.desc = "博弈池 #" + model.id;
                model.history = generateMockHistory(model.virtualReserves);
            }
        } catch (Exception e) {
            Log.e(TAG, "IPFS enrich failed for game " + model.id + ": " + e.getMessage());
            model.desc = "博弈池 #" + model.id;
            model.history = generateMockHistory(model.virtualReserves);
        }
    }

    // ========================================================================
    //  合约方法 - 读取操作（后端优先 → 链上+IPFS 回退）
    // ========================================================================

    public void getGameCount(DataCallback<Integer> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                org.web3j.abi.datatypes.Function function = buildGameCountFunction();
                String hex = ethCall(function);
                List<Type> result = FunctionReturnDecoder.decode(hex, function.getOutputParameters());
                if (result.isEmpty()) {
                    postError(callback, "市场数量解析为空");
                    return;
                }
                int count = ((Uint256) result.get(0)).getValue().intValue();
                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(count));
            } catch (Exception e) {
                postError(callback, "获取市场数量异常: " + e.getMessage());
            }
        });
    }

    /**
     * 获取单个游戏详情
     *
     * 读取策略：
     * 1. 尝试从后端 DB 获取元数据 + 缓存的链上状态（快速路径）
     * 2. 失败时回退到链上 eth_call + IPFS 直读（可靠回退路径）
     */
    @SuppressWarnings("unchecked")
    public void getGameInfo(int id, DataCallback<GameModel> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            long totalStart = System.currentTimeMillis();

            // ---- 策略 1: 尝试从后端 DB 读取（快速路径） ----
            try {
                long backendStart = System.currentTimeMillis();
                BackendApiClient.GameMetaDTO meta = BackendApiClient.fetchGameMetadata(id);
                BackendApiClient.ChainStateDTO state = BackendApiClient.fetchChainState(id, getWalletAddress());
                long backendEnd = System.currentTimeMillis();
                long backendMs = backendEnd - backendStart;
                Log.d("时延", "getGameInfo - 后端DB加载: " + backendMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("数据库+IPFS", backendMs, false));

                GameModel model = buildModelFromBackend(meta, state);

                // 如果后端元数据不完整（缺少 desc 等），从 IPFS 补充
                if (isEmpty(model.desc) || model.desc.startsWith("博弈池 #")) {
                    long ipfsStart = System.currentTimeMillis();
                    enrichFromIPFS(model);
                    long ipfsEnd = System.currentTimeMillis();
                    Log.d("时延", "getGameInfo - IPFS补充加载: " + (ipfsEnd - ipfsStart) + "ms");
                }

                long totalMs = System.currentTimeMillis() - totalStart;
                Log.d("时延", "getGameInfo - 总耗时(后端优先): " + totalMs + "ms");
                final long finalTotalMs = totalMs;
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("✅ 数据库读取成功", finalTotalMs, false);
                    callback.onSuccess(model);
                });
                return;
            } catch (Exception backendErr) {
                long failMs = System.currentTimeMillis() - totalStart;
                Log.w(TAG, "后端DB读取失败，回退到链上+IPFS: " + backendErr.getMessage());
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("数据库失败，回退链上直读...", failMs, true));
            }

            // ---- 策略 2: 回退到链上 + IPFS（可靠路径） ----
            try {
                long chainStart = System.currentTimeMillis();
                org.web3j.abi.datatypes.Function fInfo = new org.web3j.abi.datatypes.Function(
                    "getGameInfo", Collections.singletonList(new Uint256(BigInteger.valueOf(id))),
                    Arrays.asList(
                        new TypeReference<Utf8String>() {}, // ipfsCID
                        new TypeReference<Uint256>() {},    // totalPool
                        new TypeReference<Bool>() {},       // isResolved
                        new TypeReference<Uint8>() {},      // winningOption
                        new TypeReference<Uint256>() {},    // deadlineSec
                        new TypeReference<Bool>() {}        // isRefunded
                    ));

                String addr = getWalletAddress();
                if (addr == null || addr.isEmpty()) addr = "0x0000000000000000000000000000000000000000";

                org.web3j.abi.datatypes.Function fExtra = new org.web3j.abi.datatypes.Function(
                    "getGameExtraData",
                    Arrays.asList(new Uint256(BigInteger.valueOf(id)), new Address(addr)),
                    Arrays.asList(new TypeReference<DynamicArray<Uint256>>() {}, new TypeReference<DynamicArray<Uint256>>() {}));

                final String[] hexResults = new String[2];
                final java.util.concurrent.CountDownLatch latch = new java.util.concurrent.CountDownLatch(2);
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[0] = ethCall(fInfo); } catch (Exception e) { hexResults[0] = "Error: " + e.getMessage(); } finally { latch.countDown(); } });
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[1] = ethCall(fExtra); } catch (Exception e) { hexResults[1] = "Error: " + e.getMessage(); } finally { latch.countDown(); } });
                latch.await(30, java.util.concurrent.TimeUnit.SECONDS);

                long chainEnd = System.currentTimeMillis();
                long chainMs = chainEnd - chainStart;
                Log.d("时延", "getGameInfo - 链上数据加载(回退): " + chainMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("链上 eth_call", chainMs, true));

                if (hexResults[0] == null || hexResults[0].equals("0x") || hexResults[0].startsWith("Error")) {
                    postError(callback, "链上数据读取失败: " + hexResults[0]);
                    return;
                }

                List<Type> res = FunctionReturnDecoder.decode(hexResults[0], fInfo.getOutputParameters());
                if (res.isEmpty()) { postError(callback, "基础数据解析为空"); return; }

                GameModel model = new GameModel();
                model.id = id;
                model.contractAddress = contractAddress;
                model.ipfsCID = ((Utf8String) res.get(0)).getValue();
                model.totalPool = ((Uint256) res.get(1)).getValue();
                model.isResolved = ((Bool) res.get(2)).getValue();
                // 合约 winningOption: 0=NO, 1=YES → 转换为 UI 约定 0=YES, 1=NO
                model.winningOption = toUiOption(((Uint8) res.get(3)).getValue().intValue());
                model.deadlineSec = ((Uint256) res.get(4)).getValue().longValue();
                model.isRefunded = ((Bool) res.get(5)).getValue();

                model.virtualReserves = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                model.myShares = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                model.optionNames = Arrays.asList("YES", "NO");

                if (hexResults[1] != null && !hexResults[1].equals("0x") && !hexResults[1].startsWith("Error")) {
                    try {
                        List<Type> extraRes = FunctionReturnDecoder.decode(hexResults[1], fExtra.getOutputParameters());
                        if (extraRes.size() >= 2) {
                            List<Uint256> reservesArray = ((DynamicArray<Uint256>) extraRes.get(0)).getValue();
                            List<Uint256> sharesArray = ((DynamicArray<Uint256>) extraRes.get(1)).getValue();
                            if (reservesArray.size() >= 2) {
                                // 核心修复：合约返回 [reserveNO, reserveYES]。Java 索引 0 存放 reserveNO 供 UI 计算 YES 概率
                                model.virtualReserves = Arrays.asList(reservesArray.get(0).getValue(), reservesArray.get(1).getValue());
                            }
                            if (sharesArray.size() >= 2) {
                                // 合约返回 [mySharesYES, mySharesNO]。Java 索引 0 存放 YES 份额
                                model.myShares = Arrays.asList(sharesArray.get(0).getValue(), sharesArray.get(1).getValue());
                            }
                        }
                    } catch (Exception e) {
                        Log.e(TAG, "Extra data decode error for game " + id + ": " + e.getMessage());
                    }
                }

                // 从 IPFS 下载元数据
                long ipfsStart = System.currentTimeMillis();
                enrichFromIPFS(model);
                long ipfsEnd = System.currentTimeMillis();
                long ipfsMs = ipfsEnd - ipfsStart;
                Log.d("时延", "getGameInfo - IPFS加载(回退): " + ipfsMs + "ms");
                long totalMs = ipfsEnd - totalStart;
                Log.d("时延", "getGameInfo - 总耗时(回退): " + totalMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("IPFS 元数据(回退)", ipfsMs, true));

                final long finalTotalMs = totalMs;
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("⚠️ 回退链上+IPFS完成", finalTotalMs, true);
                    callback.onSuccess(model);
                });
            } catch (Exception e) {
                Log.d("时延", "getGameInfo - 失败，总耗时: " + (System.currentTimeMillis() - totalStart) + "ms");
                postError(callback, "获取详情异常: " + e.getMessage());
            }
        });
    }

    /**
     * 获取所有游戏列表
     *
     * 读取策略：后端 DB 优先 → 链上+IPFS 回退
     */
    @SuppressWarnings("unchecked")
    public void getAllGamesInfo(DataCallback<List<GameModel>> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            long totalStart = System.currentTimeMillis();

            // ---- 策略 1: 尝试从后端 DB 批量读取（快速路径） ----
            try {
                long backendStart = System.currentTimeMillis();
                List<BackendApiClient.GameMetaDTO> metas = BackendApiClient.fetchAllGameMetadata();
                List<BackendApiClient.ChainStateDTO> states = BackendApiClient.fetchAllChainStates(getWalletAddress());
                long backendEnd = System.currentTimeMillis();
                long backendMs = backendEnd - backendStart;
                Log.d("时延", "getAllGamesInfo - 后端DB加载: " + backendMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("数据库批量加载", backendMs, false));

                // 建立 gameId → ChainStateDTO 的映射
                java.util.Map<Integer, BackendApiClient.ChainStateDTO> stateMap = new java.util.HashMap<>();
                if (states != null) {
                    for (BackendApiClient.ChainStateDTO s : states) {
                        stateMap.put(s.gameId, s);
                    }
                }

                List<GameModel> models = new ArrayList<>();
                for (BackendApiClient.GameMetaDTO meta : metas) {
                    BackendApiClient.ChainStateDTO state = stateMap.get(meta.gameId);
                    GameModel m = buildModelFromBackend(meta, state);
                    if (m.history == null || m.history.isEmpty()) {
                        m.history = generateMockHistory(m.virtualReserves);
                    }
                    models.add(m);
                }

                long totalMs = System.currentTimeMillis() - totalStart;
                Log.d("时延", "getAllGamesInfo - 总耗时(后端优先): " + totalMs + "ms (共 " + models.size() + " 个)");
                final long finalTotalMs = totalMs;
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("✅ 数据库批量完成(" + models.size() + "个)", finalTotalMs, false);
                    callback.onSuccess(models);
                });
                return;
            } catch (Exception backendErr) {
                long failMs = System.currentTimeMillis() - totalStart;
                Log.w(TAG, "后端DB批量读取失败，回退到链上+IPFS: " + backendErr.getMessage());
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("⚠️ 数据库失败，回退链上批量读...", failMs, true));
            }

            // ---- 策略 2: 回退到链上 + IPFS（可靠路径） ----
            try {
                long chainStart = System.currentTimeMillis();
                org.web3j.abi.datatypes.Function fAll = new org.web3j.abi.datatypes.Function(
                        "getAllGames", Collections.emptyList(),
                        Arrays.asList(
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Utf8String>>() {},
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Bool>>() {},
                                new TypeReference<DynamicArray<Bool>>() {},
                                new TypeReference<DynamicArray<Uint8>>() {}
                        ));

                String addr = getWalletAddress();
                org.web3j.abi.datatypes.Function fExtra = new org.web3j.abi.datatypes.Function(
                        "getAllGamesExtraData",
                        Collections.singletonList(new Address(addr.isEmpty() ? "0x0000000000000000000000000000000000000000" : addr)),
                        Arrays.asList(
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Uint256>>() {},
                                new TypeReference<DynamicArray<Uint256>>() {}
                        ));

                final String[] hexResults = new String[2];
                final java.util.concurrent.CountDownLatch latch = new java.util.concurrent.CountDownLatch(2);
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[0] = ethCall(fAll); } catch (Exception ignored) {} finally { latch.countDown(); } });
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[1] = ethCall(fExtra); } catch (Exception ignored) {} finally { latch.countDown(); } });
                latch.await(30, java.util.concurrent.TimeUnit.SECONDS);

                long chainEnd = System.currentTimeMillis();
                long chainMs = chainEnd - chainStart;
                Log.d("时延", "getAllGamesInfo - 链上数据加载(回退): " + chainMs + "ms");

                if (hexResults[0] == null || hexResults[0].equals("0x")) { postError(callback, "基础数据读取失败"); return; }

                List<Type> res = FunctionReturnDecoder.decode(hexResults[0], fAll.getOutputParameters());
                List<Uint256> ids = ((DynamicArray<Uint256>) res.get(0)).getValue();
                List<Utf8String> cids = ((DynamicArray<Utf8String>) res.get(1)).getValue();

                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("链上 eth_call 批量(" + ids.size() + "个)", chainMs, true));
                List<Uint256> pools = ((DynamicArray<Uint256>) res.get(2)).getValue();
                List<Uint256> deadlines = ((DynamicArray<Uint256>) res.get(3)).getValue();
                List<Bool> isResolveds = ((DynamicArray<Bool>) res.get(4)).getValue();
                List<Bool> isRefundeds = ((DynamicArray<Bool>) res.get(5)).getValue();
                List<Uint8> winningOptions = ((DynamicArray<Uint8>) res.get(6)).getValue();

                List<Uint256> resNO = null, resYES = null, myYES = null, myNO = null;
                if (hexResults[1] != null && !hexResults[1].equals("0x")) {
                    List<Type> extraRes = FunctionReturnDecoder.decode(hexResults[1], fExtra.getOutputParameters());
                    resNO = ((DynamicArray<Uint256>) extraRes.get(0)).getValue();
                    resYES = ((DynamicArray<Uint256>) extraRes.get(1)).getValue();
                    myYES = ((DynamicArray<Uint256>) extraRes.get(2)).getValue();
                    myNO = ((DynamicArray<Uint256>) extraRes.get(3)).getValue();
                }

                List<GameModel> models = new ArrayList<>();
                for (int i = 0; i < ids.size(); i++) {
                    GameModel m = new GameModel();
                    m.id = ids.get(i).getValue().intValue();
                    m.contractAddress = contractAddress;
                    m.ipfsCID = cids.get(i).getValue();
                    m.totalPool = pools.get(i).getValue();
                    m.deadlineSec = deadlines.get(i).getValue().longValue();
                    m.isResolved = isResolveds.get(i).getValue();
                    m.isRefunded = isRefundeds.get(i).getValue();
                    // 合约 winningOption: 0=NO, 1=YES → 转换为 UI 约定 0=YES, 1=NO
                    m.winningOption = toUiOption(winningOptions.get(i).getValue().intValue());
                    m.optionNames = Arrays.asList("YES", "NO");

                    if (resNO != null && i < resNO.size()) {
                        // 核心修复：Java 索引 0 作为 YES 概率分子，需对应合约的 reserveNO
                        m.virtualReserves = Arrays.asList(resNO.get(i).getValue(), resYES.get(i).getValue());
                        m.myShares = Arrays.asList(myYES.get(i).getValue(), myNO.get(i).getValue());
                    } else {
                        m.virtualReserves = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                        m.myShares = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                    }
                    models.add(m);
                }

                // 并行从 IPFS 下载所有元数据
                long ipfsStart = System.currentTimeMillis();
                if (!models.isEmpty()) {
                    java.util.concurrent.CountDownLatch ipfsLatch = new java.util.concurrent.CountDownLatch(models.size());
                    for (GameModel m : models) {
                        AppExecutors.getInstance().networkIO().execute(() -> {
                            try {
                                String json = PinataClient.downloadJsonFromIPFS(m.ipfsCID);
                                if (json != null && !json.isEmpty()) {
                                    JSONObject obj = new JSONObject(json);
                                    m.desc = obj.optString("desc", "博弈池 #" + m.id);
                                    m.condition = obj.optString("condition", "");
                                    m.avatarUrl = obj.optString("avatarUrl", "");
                                    m.detailedInfo = obj.optString("detailedInfo", "");
                                    m.optionNames = Arrays.asList(obj.optString("optionYES", "YES"), obj.optString("optionNO", "NO"));
                                    m.optionCount = 2;
                                } else {
                                    m.desc = "博弈池 #" + m.id;
                                }
                            } catch (Exception e) {
                                m.desc = "博弈池 #" + m.id;
                            } finally {
                                ipfsLatch.countDown();
                            }
                        });
                    }
                    ipfsLatch.await(30, java.util.concurrent.TimeUnit.SECONDS);
                }
                long ipfsEnd = System.currentTimeMillis();
                long ipfsMs = ipfsEnd - ipfsStart;
                Log.d("时延", "getAllGamesInfo - IPFS数据加载(回退): " + ipfsMs + "ms");
                long totalMs = ipfsEnd - totalStart;
                Log.d("时延", "getAllGamesInfo - 总耗时(回退): " + totalMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("IPFS 批量元数据(回退)", ipfsMs, true));

                final long finalTotalMs = totalMs;
                final int modelCount = models.size();
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("⚠️ 回退链上+IPFS完成(" + modelCount + "个)", finalTotalMs, true);
                    callback.onSuccess(models);
                });
            } catch (Exception e) {
                Log.d("时延", "getAllGamesInfo - 失败，总耗时: " + (System.currentTimeMillis() - totalStart) + "ms");
                postError(callback, "批量获取市场异常: " + e.getMessage());
            }
        });
    }

    /**
     * 获取用户参与的游戏
     *
     * 读取策略：后端 DB 优先 → 链上+IPFS 回退
     */
    @SuppressWarnings("unchecked")
    public void getMyParticipatedGames(DataCallback<List<GameModel>> callback) {
        final long totalStart = System.currentTimeMillis();
        AppExecutors.getInstance().networkIO().execute(() -> {
            // ---- 策略 1: 尝试从后端 DB 读取（快速路径） ----
            try {
                long backendStart = System.currentTimeMillis();
                List<BackendApiClient.ChainStateDTO> allStates = BackendApiClient.fetchAllChainStates(getWalletAddress());
                long backendEnd = System.currentTimeMillis();
                long backendMs = backendEnd - backendStart;
                Log.d("时延", "getMyParticipatedGames - 后端DB加载: " + backendMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("数据库持仓查询", backendMs, false));

                // 过滤出用户有持仓的游戏
                List<BackendApiClient.ChainStateDTO> myStates = new ArrayList<>();
                if (allStates != null) {
                    for (BackendApiClient.ChainStateDTO s : allStates) {
                        BigInteger myYES = parseBigInteger(s.mySharesYES);
                        BigInteger myNO = parseBigInteger(s.mySharesNO);
                        if (myYES.signum() > 0 || myNO.signum() > 0) {
                            myStates.add(s);
                        }
                    }
                }

                List<GameModel> models = new ArrayList<>();
                for (BackendApiClient.ChainStateDTO state : myStates) {
                    GameModel m;
                    try {
                        BackendApiClient.GameMetaDTO meta = BackendApiClient.fetchGameMetadata(state.gameId);
                        m = buildModelFromBackend(meta, state);
                    } catch (Exception e) {
                        m = new GameModel();
                        m.id = state.gameId;
                        m.contractAddress = contractAddress;
                        m.totalPool = parseBigInteger(state.totalPool);
                        m.isResolved = state.isResolved;
                        m.isRefunded = state.isRefunded;
                        // 后端 DB 存储合约约定 (0=NO, 1=YES)，转换为 UI 约定 (0=YES, 1=NO)
                        m.winningOption = toUiOption(state.winningOption);
                        m.deadlineSec = state.deadlineSec;
                        // 核心修复：Java 索引 0 必须为 reserveNO，以匹配 UI 概率计算 (res0 / total)
                        m.virtualReserves = Arrays.asList(parseBigInteger(state.reserveNO), parseBigInteger(state.reserveYES));
                        m.myShares = Arrays.asList(parseBigInteger(state.mySharesYES), parseBigInteger(state.mySharesNO));
                        m.optionNames = Arrays.asList("YES", "NO");
                        m.desc = "博弈池 #" + state.gameId;
                    }
                    if (m.history == null || m.history.isEmpty()) {
                        m.history = generateMockHistory(m.virtualReserves);
                    }
                    models.add(m);
                }

                long totalMs = System.currentTimeMillis() - totalStart;
                Log.d("时延", "getMyParticipatedGames - 总耗时(后端优先): " + totalMs + "ms (共 " + models.size() + " 个项目)");
                final long finalTotalMs = totalMs;
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("✅ 数据库持仓完成(" + models.size() + "个)", finalTotalMs, false);
                    callback.onSuccess(models);
                });
                return;
            } catch (Exception backendErr) {
                long failMs = System.currentTimeMillis() - totalStart;
                Log.w(TAG, "后端DB读取持仓失败，回退到链上+IPFS: " + backendErr.getMessage());
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("⚠️ 数据库失败，回退链上持仓查询...", failMs, true));
            }

            // ---- 策略 2: 回退到链上 + IPFS（可靠路径） ----
            try {
                long chainStart = System.currentTimeMillis();
                String addr = getWalletAddress();
                org.web3j.abi.datatypes.Function function = new org.web3j.abi.datatypes.Function(
                        "getMyParticipatedGames",
                        Collections.singletonList(new Address(addr.isEmpty() ? "0x0000000000000000000000000000000000000000" : addr)),
                        Collections.singletonList(new TypeReference<DynamicArray<ParticipatedGameDTO>>() {})
                );

                String hex = ethCall(function);
                if (hex == null || hex.equals("0x")) {
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(new ArrayList<>()));
                    return;
                }

                List<Type> res = FunctionReturnDecoder.decode(hex, function.getOutputParameters());
                if (res.isEmpty()) {
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(new ArrayList<>()));
                    return;
                }

                List<ParticipatedGameDTO> dtos = ((DynamicArray<ParticipatedGameDTO>) res.get(0)).getValue();
                long chainEnd = System.currentTimeMillis();
                long chainMs = chainEnd - chainStart;
                Log.d("时延", "getMyParticipatedGames - 链上数据加载(回退): " + chainMs + "ms");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("链上持仓查询(" + dtos.size() + "个)", chainMs, true));

                List<GameModel> models = new ArrayList<>();
                for (ParticipatedGameDTO dto : dtos) {
                    GameModel m = new GameModel();
                    m.id = dto.id.intValue();
                    m.contractAddress = contractAddress;
                    m.ipfsCID = dto.ipfsCID;
                    m.totalPool = dto.totalPool;
                    m.deadlineSec = dto.deadlineSec.longValue();
                    m.isResolved = dto.isResolved;
                    m.isRefunded = dto.isRefunded;
                    // 合约返回 winningOption: 0=NO, 1=YES → 转换为 UI 约定 0=YES, 1=NO
                    m.winningOption = toUiOption(dto.winningOption.intValue());
                    m.optionNames = Arrays.asList("YES", "NO");
                    // 核心修复：Java 索引 0 对应 YES 概率源 (reserveNO) 和 YES 持仓 (mySharesYES)
                    m.virtualReserves = Arrays.asList(dto.reserveNO, dto.reserveYES);
                    m.myShares = Arrays.asList(dto.mySharesYES, dto.mySharesNO);
                    models.add(m);
                }

                // 并行从 IPFS 下载元数据
                long ipfsStart = System.currentTimeMillis();
                if (!models.isEmpty()) {
                    java.util.concurrent.CountDownLatch ipfsLatch = new java.util.concurrent.CountDownLatch(models.size());
                    for (GameModel m : models) {
                        AppExecutors.getInstance().networkIO().execute(() -> {
                            try {
                                String json = PinataClient.downloadJsonFromIPFS(m.ipfsCID);
                                if (json != null && !json.isEmpty()) {
                                    JSONObject obj = new JSONObject(json);
                                    m.desc = obj.optString("desc", "博弈池 #" + m.id);
                                    m.condition = obj.optString("condition", "");
                                    m.avatarUrl = obj.optString("avatarUrl", "");
                                    m.detailedInfo = obj.optString("detailedInfo", "");
                                    m.optionNames = Arrays.asList(obj.optString("optionYES", "YES"), obj.optString("optionNO", "NO"));
                                    m.optionCount = 2;
                                } else {
                                    m.desc = "博弈池 #" + m.id;
                                }
                            } catch (Exception e) {
                                m.desc = "博弈池 #" + m.id;
                            } finally {
                                ipfsLatch.countDown();
                            }
                        });
                    }
                    ipfsLatch.await(30, java.util.concurrent.TimeUnit.SECONDS);
                }
                long ipfsEnd = System.currentTimeMillis();
                long ipfsMs = ipfsEnd - ipfsStart;
                Log.d("时延", "getMyParticipatedGames - IPFS数据加载(回退): " + ipfsMs + "ms");
                long totalMs = ipfsEnd - totalStart;
                Log.d("时延", "getMyParticipatedGames - 总耗时(回退): " + totalMs + "ms (共 " + models.size() + " 个项目)");
                AppExecutors.getInstance().mainThread().execute(() ->
                    callback.onTiming("IPFS 持仓元数据(回退)", ipfsMs, true));

                final long finalTotalMs = totalMs;
                final int modelCount = models.size();
                AppExecutors.getInstance().mainThread().execute(() -> {
                    callback.onTiming("⚠️ 回退持仓完成(" + modelCount + "个)", finalTotalMs, true);
                    callback.onSuccess(models);
                });
            } catch (Exception e) {
                Log.d("时延", "getMyParticipatedGames - 失败，总耗时: " + (System.currentTimeMillis() - totalStart) + "ms");
                postError(callback, "获取参与的市场异常: " + e.getMessage());
            }
        });
    }

    // ========================================================================
    //  合约方法 - 写入操作
    //  流程：IPFS 上传 → 链上交易 → 等待确认 → eth_call 查询真实状态 → 后端 DB 同步写入 → 通知 UI
    //  设计原则：onConfirmed 回调时，后端 DB 已包含最新数据，UI 无需额外等待
    //
    //  ⚠️ 选项 ID 映射约定：
    //  合约内部使用 0=NO, 1=YES（与 getGameExtraData 的 reserves=[NO,YES] 顺序一致）
    //  Java/UI 层统一使用 0=YES, 1=NO（用户直觉：上=YES=0, 下=NO=1）
    //  所有合约调用前通过 toContractOption() 转换，所有合约返回值通过 toUiOption() 转换
    // ========================================================================

    /** UI → 合约：保持映射一致 (0=YES, 1=NO) */
    private static int toContractOption(int uiOption) {
        return uiOption;
    }

    /** 合约 → UI：保持映射一致 (0=YES, 1=NO) */
    private static int toUiOption(int contractOption) {
        return contractOption;
    }

    public void buyShares(int gameId, int optionId, BigInteger amountWei, TxCallback callback) {
        // optionId 是 UI 约定 (0=YES, 1=NO)，需转换为合约约定 (0=NO, 1=YES)
        int contractOption = toContractOption(optionId);
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "buyShares",
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(contractOption))),
            Collections.emptyList());

        // 构建交易同步信息（链上状态由 sendTransaction 在交易确认后通过 eth_call 查询真实值）
        TradeSyncInfo tradeInfo = new TradeSyncInfo();
        tradeInfo.gameId = gameId;
        tradeInfo.tradeType = "BUY";
        tradeInfo.optionId = optionId;
        tradeInfo.amountWei = amountWei.toString();

        sendTransaction(amountWei, f, "买入成功", callback, tradeInfo);
    }

    public void sellShares(int gameId, int optionId, BigInteger shareAmount, TxCallback callback) {
        // optionId 是 UI 约定 (0=YES, 1=NO)，需转换为合约约定 (0=NO, 1=YES)
        int contractOption = toContractOption(optionId);
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "sellShares", Arrays.asList(new Uint256(gameId), new Uint8(contractOption), new Uint256(shareAmount)), Collections.emptyList());

        // 构建交易同步信息（链上状态由 sendTransaction 在交易确认后通过 eth_call 查询真实值）
        TradeSyncInfo tradeInfo = new TradeSyncInfo();
        tradeInfo.gameId = gameId;
        tradeInfo.tradeType = "SELL";
        tradeInfo.optionId = optionId;
        tradeInfo.amountWei = shareAmount.toString();

        sendTransaction(BigInteger.ZERO, f, "卖出成功", callback, tradeInfo);
    }

    /**
     * 创建博弈池
     *
     * 写入流程（四步串行）：
     * 1. 上传图片到 IPFS（如有）
     * 2. 上传元数据 JSON 到 IPFS
     * 3. 带 IPFS CID 发送 createGame 链上交易，等待确认
     * 4. 交易确认后 → 查询链上状态 → 同步元数据 + 链上状态到后端 DB → 通知 UI
     */
    public void createGame(String desc, String condition, byte[] imageData,
                           String detailedInfo, List<String> optionNamesList,
                           long duration, BigInteger initialLiquidityWei, TxCallback callback) {

        AppExecutors.getInstance().networkIO().execute(() -> {
            String avatarCid = "";
            String metadataCid = "";
            int newGameId = 0;
            try {
                // ---- 步骤 1: 上传图片到 IPFS ----
                if (imageData != null && imageData.length > 0) {
                    try {
                        avatarCid = PinataClient.uploadFileToIPFS(imageData, "avatar.png", "image/png");
                        Log.d(TAG, "图片上传到IPFS成功, CID: " + avatarCid);
                    } catch (Exception e) {
                        Log.e(TAG, "图片上传到IPFS失败: " + e.getMessage());
                    }
                }

                // ---- 步骤 2: 上传元数据 JSON 到 IPFS ----
                JSONObject metadata = new JSONObject();
                metadata.put("desc", desc);
                metadata.put("condition", condition);
                metadata.put("avatarUrl", avatarCid);
                metadata.put("detailedInfo", detailedInfo);
                metadata.put("optionYES", optionNamesList.get(0));
                metadata.put("optionNO", optionNamesList.get(1));

                metadataCid = PinataClient.uploadJsonToIPFS(metadata);
                Log.d(TAG, "元数据上传到IPFS成功, CID: " + metadataCid);

                // ---- 步骤 3: 发送 createGame 链上交易 ----
                // duration 已经是秒（由上层 (end - now) / 1000 计算），合约 createGame 的 _durationSec 参数期望秒
                org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
                    "createGame",
                    Arrays.asList(new Utf8String(metadataCid), new Uint256(duration)),
                    Collections.emptyList());

                String data = FunctionEncoder.encode(f);
                String txHash;
                if (useLocalRpc) {
                    txHash = sendLocalRpcAndWait(initialLiquidityWei, data, callback);
                    if (txHash == null) return;
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onTxSent(txHash));
                } else {
                    txHash = sendBrokerChainTx(initialLiquidityWei, data, callback);
                    if (txHash == null) return;
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onTxSent("Transaction Sent"));
                    Thread.sleep(8000); // 等待链上确认
                }

                // ---- 步骤 4: 同步到后端 DB（在 onConfirmed 之前） ----
                final String finalAvatarCid = avatarCid;
                final String finalMetadataCid = metadataCid;
                final String finalTxHash = txHash;

                // 4a. 获取新创建博弈池的 gameId（通过 gameCount 推算）
                try {
                    org.web3j.abi.datatypes.Function fCount = buildGameCountFunction();
                    String hex = ethCall(fCount);
                    List<Type> result = FunctionReturnDecoder.decode(hex, fCount.getOutputParameters());
                    if (!result.isEmpty()) {
                        newGameId = ((Uint256) result.get(0)).getValue().intValue();
                    }
                } catch (Exception e) {
                    Log.w(TAG, "获取 gameCount 失败，gameId 传 0 由后端自行解析");
                }

                // 4b. 同步游戏元数据
                try {
                    BackendApiClient.GameMetaSyncReq syncReq = new BackendApiClient.GameMetaSyncReq();
                    syncReq.gameId = newGameId;
                    syncReq.contractAddress = contractAddress;
                    syncReq.ipfsCid = finalMetadataCid;
                    syncReq.desc = desc;
                    syncReq.condition = condition;
                    syncReq.avatarUrl = finalAvatarCid;
                    syncReq.detailedInfo = detailedInfo;
                    syncReq.optionYES = optionNamesList.get(0);
                    syncReq.optionNO = optionNamesList.get(1);
                    syncReq.creatorAddress = walletAddress;
                    syncReq.durationSec = duration;
                    syncReq.initialLiquidityWei = initialLiquidityWei.toString();
                    BackendApiClient.syncGameMetadata(syncReq);
                    Log.d(TAG, "✅ 创建博弈池 - 后端元数据同步成功: gameId=" + newGameId);
                } catch (Exception e) {
                    Log.w(TAG, "❌ 创建博弈池 - 后端元数据同步失败（非关键）: " + e.getMessage());
                }

                // 4c. 查询初始链上状态并同步到后端 DB
                if (newGameId > 0) {
                    try {
                        // 短暂延迟确保链上状态已更新
                        Thread.sleep(2000);
                        PostTxState postState = queryPostTxState(newGameId);
                        if (postState != null) {
                            BackendApiClient.ChainStateSyncReq chainReq = new BackendApiClient.ChainStateSyncReq();
                            chainReq.totalPool = postState.totalPool;
                            chainReq.isResolved = false;
                            chainReq.isRefunded = false;
                            chainReq.winningOption = 0;
                            chainReq.deadlineSec = postState.deadlineSec;
                            chainReq.reserveYES = postState.reserveYES;
                            chainReq.reserveNO = postState.reserveNO;
                            chainReq.mySharesYES = "0";
                            chainReq.mySharesNO = "0";
                            BackendApiClient.syncChainState(newGameId, chainReq);
                            Log.d(TAG, "✅ 创建博弈池 - 后端初始链上状态同步成功: gameId=" + newGameId
                                    + " deadlineSec=" + postState.deadlineSec);
                        }
                    } catch (Exception e) {
                        Log.w(TAG, "❌ 创建博弈池 - 后端链上状态同步失败（非关键）: " + e.getMessage());
                    }

                    // 4d. 添加初始历史价格点
                    try {
                        BackendApiClient.HistoryPointDTO point = new BackendApiClient.HistoryPointDTO();
                        point.gameId = newGameId;
                        point.timestampSec = System.currentTimeMillis() / 1000;
                        point.yesPrice = 50f;
                        point.noPrice = 50f;
                        point.totalPool = initialLiquidityWei.toString();
                        BackendApiClient.addHistoryPoint(newGameId, point);
                        Log.d(TAG, "✅ 创建博弈池 - 初始历史价格点同步成功: gameId=" + newGameId);
                    } catch (Exception e) {
                        Log.w(TAG, "❌ 创建博弈池 - 初始历史价格点同步失败（非关键）: " + e.getMessage());
                    }
                }

                // ---- 通知 UI 完成（后端 DB 已更新） ----
                final int finalGameId = newGameId;
                AppExecutors.getInstance().mainThread().execute(() -> callback.onConfirmed("博弈池部署成功 (ID=" + finalGameId + ")"));

            } catch (Exception e) {
                e.printStackTrace();
                postError(callback, "IPFS 上传失败: " + e.getMessage());
            }
        });
    }

    public BigDecimal calculateShuibeiPrice(BigDecimal basePrice, boolean isBuy) {
        BigDecimal spread = new BigDecimal("0.005");
        if (isBuy) {
            return basePrice.multiply(BigDecimal.ONE.add(spread));
        } else {
            return basePrice.multiply(BigDecimal.ONE.subtract(spread));
        }
    }

    public void claimReward(int gameId, int optionId, TxCallback callback) {
        TradeSyncInfo tradeInfo = new TradeSyncInfo();
        tradeInfo.gameId = gameId;
        tradeInfo.tradeType = "CLAIM";
        tradeInfo.optionId = optionId;
        tradeInfo.amountWei = "0";

        sendTransaction(BigInteger.ZERO, buildClaimRewardFunction(gameId, optionId), "领取成功", callback, tradeInfo);
    }

    public void resolveGame(int gameId, int winningOption, TxCallback callback) {
        // winningOption 是 UI 约定 (0=YES, 1=NO)，需转换为合约约定 (0=NO, 1=YES)
        int contractOption = toContractOption(winningOption);
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "resolveGame",
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(contractOption))),
            Collections.emptyList());

        TradeSyncInfo tradeInfo = new TradeSyncInfo();
        tradeInfo.gameId = gameId;
        tradeInfo.tradeType = "RESOLVE";
        tradeInfo.optionId = winningOption;          // 交易记录用 UI 约定 (0=YES, 1=NO)
        tradeInfo.amountWei = "0";
        tradeInfo.isResolved = true;
        // 后端 DB 的 winning_option 使用合约约定 (0=NO, 1=YES)，与 syncChainState 一致
        tradeInfo.winningOption = contractOption;

        sendTransaction(BigInteger.ZERO, f, "开奖成功", callback, tradeInfo);
    }

    // ========================================================================
    //  AI 托管状态 API（通过后端 DB）
    // ========================================================================

    public void toggleAiManaged(int gameId, boolean enabled, DataCallback<Boolean> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                boolean result = BackendApiClient.setAiManagedStatus(
                        gameId, getWalletAddress(), enabled, contractAddress, privateKey);
                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(result ? enabled : !enabled));
            } catch (Exception e) {
                // 回退到原有直连方式
                try {
                    JSONObject json = new JSONObject();
                    json.put("game_id", gameId);
                    json.put("user_address", getWalletAddress());
                    json.put("enabled", enabled);
                    json.put("contract_address", contractAddress);
                    json.put("private_key", privateKey);

                    URL url = new URL("https://dash.broker-chain.com:440/api/gold/ai-managed");
                    java.net.HttpURLConnection conn = (java.net.HttpURLConnection) url.openConnection();
                    conn.setRequestMethod("POST");
                    conn.setRequestProperty("Content-Type", "application/json");
                    conn.setDoOutput(true);
                    try (java.io.OutputStream os = conn.getOutputStream()) {
                        os.write(json.toString().getBytes(java.nio.charset.StandardCharsets.UTF_8));
                    }
                    int code = conn.getResponseCode();
                    if (code == 200) {
                        AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(enabled));
                    } else {
                        postError(callback, "后端响应错误: " + code);
                    }
                } catch (Exception ex) {
                    postError(callback, "通知后端失败: " + ex.getMessage());
                }
            }
        });
    }

    public void getAiManagedStatus(int gameId, DataCallback<Boolean> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                boolean enabled = BackendApiClient.getAiManagedStatus(
                        gameId, getWalletAddress(), contractAddress);
                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(enabled));
            } catch (Exception e) {
                // 回退到原有直连方式
                try {
                    String path = String.format("/api/gold/ai-managed?game_id=%d&user_address=%s", gameId, getWalletAddress());
                    URL url = new URL("https://dash.broker-chain.com:440" + path);
                    java.net.HttpURLConnection conn = (java.net.HttpURLConnection) url.openConnection();
                    conn.setRequestMethod("GET");
                    if (conn.getResponseCode() == 200) {
                        try (java.util.Scanner s = new java.util.Scanner(conn.getInputStream())) {
                            String resp = s.useDelimiter("\\A").hasNext() ? s.next() : "{}";
                            JSONObject obj = new JSONObject(resp);
                            boolean enabled = obj.optBoolean("enabled", false);
                            AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(enabled));
                        }
                    } else {
                        AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(false));
                    }
                } catch (Exception ex) {
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(false));
                }
            }
        });
    }

    // ── 模拟历史数据 ──

    private List<HistoryPoint> generateMockHistory(List<BigInteger> reserves) {
        List<HistoryPoint> list = new ArrayList<>();
        if (reserves == null || reserves.size() < 2) return list;

        double yes = reserves.get(0).doubleValue();
        double no = reserves.get(1).doubleValue();
        double total = yes + no;
        if (total <= 0) return list;

        float currentYesPct = (float)(yes / total * 100);
        long now = System.currentTimeMillis() / 1000;

        for (int i = 7; i >= 0; i--) {
            HistoryPoint p = new HistoryPoint();
            p.time = now - (long)i * 86400;
            float noise = (float)((Math.random() - 0.5) * 10 * (i / 7.0));
            p.yesPrice = Math.max(5, Math.min(95, currentYesPct + noise));
            p.noPrice = 100 - p.yesPrice;
            list.add(p);
        }
        return list;
    }

    // ── 工具方法 ──

    private boolean isEmpty(String s) {
        return s == null || s.trim().isEmpty();
    }

    private void postError(TxCallback callback, String error) {
        AppExecutors.getInstance().mainThread().execute(() -> callback.onError(error));
    }
    private <T> void postError(DataCallback<T> callback, String error) {
        AppExecutors.getInstance().mainThread().execute(() -> callback.onError(error));
    }

    // ========================================================================
    //  数据模型
    // ========================================================================

    public static class GameModel {
        public int id;
        public String contractAddress;
        public String ipfsCID;          // IPFS 上存储元数据的 CID
        public String desc, condition, avatarUrl, detailedInfo;
        public List<String> optionNames;
        public int optionCount;
        public BigInteger totalPool;
        public boolean isResolved, isRefunded;
        public int winningOption;
        public long deadlineSec;
        public List<BigInteger> virtualReserves, myShares;
        public boolean isManaged;
        public List<HistoryPoint> history;
    }

    public static class HistoryPoint {
        public long time;
        public float yesPrice;
        public float noPrice;
    }

    /**
     * 交易同步信息（用于 tx 确认后同步到后端 DB）
     */
    private static class TradeSyncInfo {
        int gameId;
        String tradeType;       // "BUY", "SELL", "CLAIM", "RESOLVE"
        int optionId;
        String amountWei;
        // 交易后的链上状态（可选，后端可自行从链上刷新）
        String totalPoolAfter;
        String reserveYESAfter;
        String reserveNOAfter;
        String mySharesYESAfter;
        String mySharesNOAfter;
        long deadlineSec;       // 链上绝对截止时间戳（秒），确保后端 DB 不丢失截止时间
        // 结算状态（用于同步链上状态缓存到后端 DB）
        boolean isResolved;
        boolean isRefunded;
        int winningOption;
    }

    public static class ParticipatedGameDTO extends DynamicStruct {
        public BigInteger id;
        public String ipfsCID;
        public BigInteger totalPool;
        public BigInteger deadlineSec;
        public Boolean isResolved;
        public Boolean isRefunded;
        public BigInteger winningOption;
        public BigInteger reserveNO;
        public BigInteger reserveYES;
        public BigInteger mySharesYES;
        public BigInteger mySharesNO;

        public ParticipatedGameDTO(Uint256 id, Utf8String ipfsCID, Uint256 totalPool, Uint256 deadlineSec, Bool isResolved, Bool isRefunded, Uint8 winningOption, Uint256 reserveNO, Uint256 reserveYES, Uint256 mySharesYES, Uint256 mySharesNO) {
            super(id, ipfsCID, totalPool, deadlineSec, isResolved, isRefunded, winningOption, reserveNO, reserveYES, mySharesYES, mySharesNO);
            this.id = id.getValue();
            this.ipfsCID = ipfsCID.getValue();
            this.totalPool = totalPool.getValue();
            this.deadlineSec = deadlineSec.getValue();
            this.isResolved = isResolved.getValue();
            this.isRefunded = isRefunded.getValue();
            this.winningOption = winningOption.getValue();
            this.reserveNO = reserveNO.getValue();
            this.reserveYES = reserveYES.getValue();
            this.mySharesYES = mySharesYES.getValue();
            this.mySharesNO = mySharesNO.getValue();
        }
    }
}
