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
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

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

    public static org.web3j.abi.datatypes.Function buildClaimRewardFunction(int gameId, int optionId) {
        return new org.web3j.abi.datatypes.Function(
            "claimReward",
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(optionId))),
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
                from,
                null,
                LOCAL_RPC_CALL_GAS_PRICE,
                LOCAL_RPC_CALL_GAS_LIMIT,
                to,
                LOCAL_RPC_CALL_VALUE,
                data);
    }

    static Transaction buildLocalWriteTransaction(String from, String to, String data, BigInteger value) {
        return Transaction.createFunctionCallTransaction(
                from,
                null,
                LOCAL_RPC_CALL_GAS_PRICE,
                LOCAL_RPC_CALL_GAS_LIMIT,
                to,
                value,
                data);
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
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                String data = FunctionEncoder.encode(function);
                if (useLocalRpc) {
                    standardSendTx(value, data, successMsg, callback);
                } else {
                    brokerChainSendTx(value, data, successMsg, callback);
                }
            } catch (Exception e) {
                postError(callback, "交易异常: " + e.getMessage());
            }
        });
    }

    private void standardSendTx(BigInteger value, String data, String successMsg, TxCallback callback) throws Exception {
        Transaction txn = buildLocalWriteTransaction(walletAddress, contractAddress, data, value);
        Log.d(TAG, "standard eth_sendTransaction to=" + contractAddress
                + " value=" + txn.getValue()
                + " data=" + data.substring(0, Math.min(66, data.length())) + "...");
        EthSendTransaction resp = web3j.ethSendTransaction(txn).send();
        if (resp.hasError()) {
            postError(callback, resp.getError().getMessage());
        } else if (resp.getTransactionHash() == null || resp.getTransactionHash().isEmpty()) {
            postError(callback, "本地 RPC 未返回交易哈希，交易未确认提交");
        } else {
            String txHash = resp.getTransactionHash();
            Log.d(TAG, "standard eth_sendTransaction hash=" + txHash);
            AppExecutors.getInstance().mainThread().execute(() -> {
                callback.onTxSent(txHash);
            });
            waitForLocalReceipt(txHash);
            AppExecutors.getInstance().mainThread().execute(() -> {
                callback.onConfirmed(successMsg);
            });
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

    private void brokerChainSendTx(BigInteger value, String data, String successMsg, TxCallback callback) throws Exception {
        String valueHex = value.compareTo(BigInteger.ZERO) > 0 ? value.toString(16) : "0x0";
        String response = BrokerChainClient.sendEthTx(privateKey, contractAddress, data, valueHex);
        if (response == null || response.toLowerCase().contains("error") || response.toLowerCase().contains("failed")) {
            postError(callback, "交易失败: " + response);
        } else {
            AppExecutors.getInstance().mainThread().execute(() -> {
                callback.onTxSent("Transaction Sent");
                callback.onConfirmed(successMsg);
            });
        }
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

    // ── contract methods ──

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

    @SuppressWarnings("unchecked")
    public void getGameInfo(int id, DataCallback<GameModel> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                // 1. 准备链上数据请求
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

                // 2. 并发执行链上请求
                final String[] hexResults = new String[2];
                final java.util.concurrent.CountDownLatch latch = new java.util.concurrent.CountDownLatch(2);
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[0] = ethCall(fInfo); } catch (Exception e) { hexResults[0] = "Error: " + e.getMessage(); } finally { latch.countDown(); } });
                AppExecutors.getInstance().networkIO().execute(() -> { try { hexResults[1] = ethCall(fExtra); } catch (Exception e) { hexResults[1] = "Error: " + e.getMessage(); } finally { latch.countDown(); } });
                latch.await(30, java.util.concurrent.TimeUnit.SECONDS);

                if (hexResults[0] == null || hexResults[0].equals("0x") || hexResults[0].startsWith("Error")) { 
                    postError(callback, "链上数据读取失败: " + hexResults[0]); 
                    return; 
                }

                // 3. 解析链上基础数据
                List<Type> res = FunctionReturnDecoder.decode(hexResults[0], fInfo.getOutputParameters());
                if (res.isEmpty()) { postError(callback, "基础数据解析为空"); return; }
                
                GameModel model = new GameModel();
                model.id = id;
                model.contractAddress = contractAddress;
                model.ipfsCID = ((Utf8String) res.get(0)).getValue();
                model.totalPool = ((Uint256) res.get(1)).getValue();
                model.isResolved = ((Bool) res.get(2)).getValue();
                model.winningOption = ((Uint8) res.get(3)).getValue().intValue();
                model.deadlineSec = ((Uint256) res.get(4)).getValue().longValue();
                model.isRefunded = ((Bool) res.get(5)).getValue();

                // 统一初始化，避免 UI 端判断 null
                model.virtualReserves = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                model.myShares = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                model.optionNames = Arrays.asList("YES", "NO");

                // 4. 解析持仓数据
                if (hexResults[1] != null && !hexResults[1].equals("0x") && !hexResults[1].startsWith("Error")) {
                    try {
                        List<Type> extraRes = FunctionReturnDecoder.decode(hexResults[1], fExtra.getOutputParameters());
                        if (extraRes.size() >= 2) {
                            List<Uint256> reservesArray = ((DynamicArray<Uint256>) extraRes.get(0)).getValue();
                            List<Uint256> sharesArray = ((DynamicArray<Uint256>) extraRes.get(1)).getValue();
                            
                            // 合约顺序: 0: reserveNO, 1: reserveYES -> UI 习惯: 0: YES, 1: NO
                            if (reservesArray.size() >= 2) {
                                model.virtualReserves = Arrays.asList(reservesArray.get(1).getValue(), reservesArray.get(0).getValue());
                            }
                            
                            // 合约顺序: userShares[gameId][user][0] (YES), [1] (NO) -> UI 习惯: 0: YES, 1: NO
                            if (sharesArray.size() >= 2) {
                                model.myShares = Arrays.asList(sharesArray.get(0).getValue(), sharesArray.get(1).getValue());
                            }
                        }
                    } catch (Exception e) {
                        Log.e(TAG, "Extra data decode error for game " + id + ": " + e.getMessage());
                    }
                }

                // 5. 下载文本元数据
                try {
                    String ipfsJsonStr = PinataClient.downloadJsonFromIPFS(model.ipfsCID);
                    Log.d("getGameInfo","Json数据"+ipfsJsonStr);
                    if (ipfsJsonStr != null && !ipfsJsonStr.isEmpty()) {
                        JSONObject ipfsData = new JSONObject(ipfsJsonStr);
                        model.desc = ipfsData.optString("desc", "博弈池 #" + id);
                        model.condition = ipfsData.optString("condition", "暂无详细判定逻辑");
                        model.avatarUrl = ipfsData.optString("avatarUrl", "");
                        model.detailedInfo = ipfsData.optString("detailedInfo", "");
                        model.optionNames = Arrays.asList(ipfsData.optString("optionYES", "YES"), ipfsData.optString("optionNO", "NO"));
                        model.optionCount = 2;
                    } else {
                        model.desc = "博弈池 #" + id;
                    }
                } catch (Exception e) {
                    Log.e(TAG, "IPFS 数据下载失败: " + e.getMessage());
                    model.desc = "博弈池 #" + id;
                }

                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(model));
            } catch (Exception e) {
                postError(callback, "获取详情异常: " + e.getMessage());
            }
        });
    }

    @SuppressWarnings("unchecked")
    public void getAllGamesInfo(DataCallback<List<GameModel>> callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
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

                if (hexResults[0] == null || hexResults[0].equals("0x")) { postError(callback, "基础数据读取失败"); return; }

                List<Type> res = FunctionReturnDecoder.decode(hexResults[0], fAll.getOutputParameters());
                List<Uint256> ids = ((DynamicArray<Uint256>) res.get(0)).getValue();
                List<Utf8String> cids = ((DynamicArray<Utf8String>) res.get(1)).getValue();
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
                    m.winningOption = winningOptions.get(i).getValue().intValue();
                    m.optionNames = Arrays.asList("YES", "NO");

                    if (resNO != null && i < resNO.size()) {
                        // 合约顺序: 0: reserveNO, 1: reserveYES -> UI 习惯: 0: YES, 1: NO
                        m.virtualReserves = Arrays.asList(resYES.get(i).getValue(), resNO.get(i).getValue());
                        // 合约顺序: myYES, myNO -> UI 习惯: 0: YES, 1: NO
                        m.myShares = Arrays.asList(myYES.get(i).getValue(), myNO.get(i).getValue());
                    } else {
                        m.virtualReserves = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                        m.myShares = Arrays.asList(BigInteger.ZERO, BigInteger.ZERO);
                    }
                    models.add(m);
                }

                if (!models.isEmpty()) {
                    long ipfsStartTime = System.currentTimeMillis();
                    java.util.concurrent.CountDownLatch ipfsLatch = new java.util.concurrent.CountDownLatch(models.size());
                    for (GameModel m : models) {
                        AppExecutors.getInstance().networkIO().execute(() -> {
                            try {
                                String json = PinataClient.downloadJsonFromIPFS(m.ipfsCID);
                                Log.d("getAllGamesInfo","Json"+json);
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
                    long ipfsEndTime = System.currentTimeMillis();
                    Log.d("LatencyDebug", "getAllGamesInfo - IPFS 元数据(并行)加载耗时: " + (ipfsEndTime - ipfsStartTime) + "ms");
                }

                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(models));
            } catch (Exception e) {
                postError(callback, "批量获取市场异常: " + e.getMessage());
            }
        });
    }

    @SuppressWarnings("unchecked")
    public void getMyParticipatedGames(DataCallback<List<GameModel>> callback) {
        final long startTime = System.currentTimeMillis();
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
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
                    m.winningOption = dto.winningOption.intValue();
                    m.optionNames = Arrays.asList("YES", "NO");
                    
                    // 合约 DTO 顺序: reserveNO, reserveYES, mySharesYES, mySharesNO
                    // UI 统一顺序: 0: YES, 1: NO
                    m.virtualReserves = Arrays.asList(dto.reserveYES, dto.reserveNO);
                    m.myShares = Arrays.asList(dto.mySharesYES, dto.mySharesNO);
                    models.add(m);
                }

                if (!models.isEmpty()) {
                    long ipfsStartTime = System.currentTimeMillis();
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
                    long ipfsEndTime = System.currentTimeMillis();
                    Log.d("LatencyDebug", "getMyParticipatedGames - IPFS 元数据(并行)加载耗时: " + (ipfsEndTime - ipfsStartTime) + "ms");
                }

                Log.d("LatencyDebug", "getMyParticipatedGames - 总流程耗时: " + (System.currentTimeMillis() - startTime) + "ms (共 " + models.size() + " 个项目)");
                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(models));
            } catch (Exception e) {
                postError(callback, "获取参与的市场异常: " + e.getMessage());
            }
        });
    }


    public void buyShares(int gameId, int optionId, BigInteger amountWei, TxCallback callback) {
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "buyShares", 
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(optionId))), 
            Collections.emptyList());
        sendTransaction(amountWei, f, "买入成功", callback);
    }

    public void sellShares(int gameId, int optionId, BigInteger shareAmount, TxCallback callback) {
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "sellShares", Arrays.asList(new Uint256(gameId), new Uint8(optionId), new Uint256(shareAmount)), Collections.emptyList());
        sendTransaction(BigInteger.ZERO, f, "卖出成功", callback);
    }

    public void createGame(String desc, String condition, String avatarUrl,
                           String detailedInfo, List<String> optionNamesList,
                           long duration, BigInteger initialLiquidityWei, TxCallback callback) {
        
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                // 1. 先将元数据上传到 IPFS
                JSONObject metadata = new JSONObject();
                metadata.put("desc", desc);
                metadata.put("condition", condition);
                metadata.put("avatarUrl", avatarUrl);
                metadata.put("detailedInfo", detailedInfo);
                // 存储选项名称，虽然目前固定是 YES/NO
                metadata.put("optionYES", optionNamesList.get(0));
                metadata.put("optionNO", optionNamesList.get(1));

                String cid = PinataClient.uploadJsonToIPFS(metadata);
                Log.d(TAG, "成功上传 IPFS, CID: " + cid);

                // 核心修复：根据环境自动调整时间单位
                // 关键判断：如果不是本地 RPC (即 BrokerChain)，且传入的 duration 看起来像秒级 (小于 10^10)，
                // 考虑到 BrokerChain block.timestamp 通常是毫秒级的，这里必须乘以 1000。
                long finalDuration = duration;
                if (!useLocalRpc && duration < 10_000_000_000L) {
                    finalDuration = duration * 1000L;
                }

                // 2. 带着 CID 上链
                org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
                    "createGame", 
                    Arrays.asList(new Utf8String(cid), new Uint256(finalDuration)),
                    Collections.emptyList());
                    
                sendTransaction(initialLiquidityWei, f, "博弈池部署成功", callback);

            } catch (Exception e) {
                e.printStackTrace();
                postError(callback, "IPFS 上传失败: " + e.getMessage());
            }
        });
    }

    /**
     * 获取水贝模式下的实时点差。
     * 模拟深圳水贝黄金交易：买入价稍高于基准，卖出价稍低于基准。
     */
    public BigDecimal calculateShuibeiPrice(BigDecimal basePrice, boolean isBuy) {
        BigDecimal spread = new BigDecimal("0.005"); // 0.5% 点差
        if (isBuy) {
            return basePrice.multiply(BigDecimal.ONE.add(spread));
        } else {
            return basePrice.multiply(BigDecimal.ONE.subtract(spread));
        }
    }

    public void claimReward(int gameId, int optionId, TxCallback callback) {
        sendTransaction(BigInteger.ZERO, buildClaimRewardFunction(gameId, optionId), "领取成功", callback);
    }

    /**
     * 管理员/后台开奖接口
     * 将 App 判定后的获胜选项同步到链上，使用户可以领奖
     */
    public void resolveGame(int gameId, int winningOption, TxCallback callback) {
        org.web3j.abi.datatypes.Function f = new org.web3j.abi.datatypes.Function(
            "resolveGame", 
            Arrays.asList(new Uint256(BigInteger.valueOf(gameId)), new Uint8(BigInteger.valueOf(winningOption))),
            Collections.emptyList());
        sendTransaction(BigInteger.ZERO, f, "开奖成功", callback);
    }

    private void postError(TxCallback callback, String error) {
        AppExecutors.getInstance().mainThread().execute(() -> callback.onError(error));
    }
    private <T> void postError(DataCallback<T> callback, String error) {
        AppExecutors.getInstance().mainThread().execute(() -> callback.onError(error));
    }

    public static class GameModel {
        public int id;
        public String contractAddress;
        public String ipfsCID; // 链上存储的 IPFS 哈希
        // 下面这些将从 IPFS 加载
        public String desc, condition, avatarUrl, detailedInfo;
        public List<String> optionNames;
        public int optionCount;
        public BigInteger totalPool;
        public boolean isResolved, isRefunded;
        public int winningOption;
        public long deadlineSec;
        public List<BigInteger> virtualReserves, myShares;
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
