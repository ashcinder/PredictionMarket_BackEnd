package com.example.brokerfi.xc.agent.model;

import androidx.appcompat.app.AppCompatActivity;

import com.example.brokerfi.xc.MyUtil;
import com.example.brokerfi.xc.StorageUtil;
import com.example.brokerfi.xc.net.ABIUtils;
import com.google.gson.Gson;
import android.util.Log;

import com.google.gson.reflect.TypeToken;

import java.lang.reflect.Type;
import java.util.Map;

/**
 * AI Agent 主控制器 — 单例，编排 Wallet 现有 API + DeepSeek AI 分析。
 * 不修改任何 Wallet 底层代码，只做只读调用和策略建议。
 */
public class AgentManager {
    private final Gson gson = new Gson();

    private AgentManager() {}

    private static class Holder {
        static final AgentManager INSTANCE = new AgentManager();
    }

    public static AgentManager getInstance() {
        return Holder.INSTANCE;
    }

    // =============== Broker 收益分析 ===============

    /**
     * 查询各 shard 收益并让 AI 给出调仓建议。
     * 返回的 BrokerReport 可直接展示给用户。
     */
    public void analyzeBroker(AppCompatActivity activity, AnalysisCallback callback) {
        String pk = StorageUtil.getCurrentPrivatekey(activity);
        if (pk == null || pk.isEmpty()) {
            callback.onError("No account selected");
            return;
        }

        new Thread(() -> {
            try {
                String profitJson = MyUtil.querybrokerprofit(pk);
                if (profitJson == null || profitJson.isEmpty()) {
                    callback.onError("Failed to query broker profit");
                    return;
                }

                // 解析 shard 数据
                ShardProfit[] shards = parseShardProfits(profitJson);

                // 构建 prompt
                StringBuilder sb = new StringBuilder();
                sb.append("分析以下经纪分片质押数据，给出调仓建议：\n");
                for (ShardProfit s : shards) {
                    sb.append(String.format("- 分片 %d: 收益=%.4f BKC, 质押=%.0f BKC, 收益率=%.2f%%\n",
                            s.shardIndex, s.profit, s.staked, s.yieldPct * 100));
                }
                sb.append("\n请给出：1) 建议从哪个分片撤出，2) 建议加仓哪个分片，3) 预估改善幅度。150字以内。");

                DeepSeekClient.chat(
                        "你是一个DeFi质押策略分析师。请根据分片收益数据，用中文给出具体、编号的建议。",
                        sb.toString(),
                        new DeepSeekClient.ChatCallback() {
                            @Override
                            public void onSuccess(String response) {
                                BrokerReport report = new BrokerReport();
                                report.rawAnalysis = response;
                                report.shards = shards;
                                callback.onBrokerReport(report);
                            }

                            @Override
                            public void onError(String error) {
                                // 即使 AI 不可用，也返回原始数据
                                BrokerReport report = new BrokerReport();
                                report.rawAnalysis = "AI分析不可用，以下为原始数据：";
                                report.shards = shards;
                                callback.onBrokerReport(report);
                            }
                        });
            } catch (Exception e) {
                callback.onError(e.getMessage());
            }
        }).start();
    }

    // =============== NFT 市场 AI 推荐 ===============

    public void recommendNFTs(AppCompatActivity activity, AnalysisCallback callback) {
        String pk = StorageUtil.getCurrentPrivatekey(activity);
        if (pk == null || pk.isEmpty()) {
            callback.onError("No account selected");
            return;
        }

        new Thread(() -> {
            try {
                String data = ABIUtils.encodeGetListedNFTs();
                String hexResult = MyUtil.sendethcall(data, pk);

                ABIUtils.ListedNFTsResult result = ABIUtils.decodeGetListedNFTs(hexResult);
                if (result == null || result.names == null || result.names.length == 0) {
                    callback.onError("No NFTs listed");
                    return;
                }

                StringBuilder sb = new StringBuilder();
                sb.append("分析以下已上架NFT，推荐值得购买的：\n");
                int count = Math.min(result.names.length, 20);
                for (int i = 0; i < count; i++) {
                    sb.append(String.format("- #%d: %s, 价格=%s, 份数=%s\n",
                            result.nftIds[i], result.names[i],
                            result.pricesList[i], result.sharesList[i]));
                }
                sb.append("\n推荐前3个并给出理由。150字以内。");

                DeepSeekClient.chatSimple(sb.toString(), new DeepSeekClient.ChatCallback() {
                    @Override
                    public void onSuccess(String response) {
                        callback.onGeneralAdvice("NFT Market Analysis", response);
                    }

                    @Override
                    public void onError(String error) {
                        callback.onGeneralAdvice("NFT市场（离线模式）", "已发现 " + result.names.length + " 个上架NFT。");
                    }
                });
            } catch (Exception e) {
                callback.onError(e.getMessage());
            }
        }).start();
    }

    // =============== 通用对话 ===============

    public void askAnything(String question, AnalysisCallback callback) {
        if (containsPrivateKey(question)) {
            callback.onError("您的消息可能包含私钥或助记词，为保障安全，未发送至AI。");
            return;
        }
        DeepSeekClient.chatSimple(question, new DeepSeekClient.ChatCallback() {
            @Override
            public void onSuccess(String response) {
                callback.onGeneralAdvice(question, response);
            }

            @Override
            public void onError(String error) {
                callback.onError(error);
            }
        });
    }

    public void askGoldResearch(String question, AnalysisCallback callback) {
        if (containsPrivateKey(question)) {
            callback.onError("您的消息可能包含私钥或助记词，为保障安全，未发送至AI。");
            return;
        }
        DeepSeekClient.chat(
                "你是 BrokerChain 黄金票据预测市场的 AI 投研助手。"
                        + "只围绕黄金现货、美元、避险需求、链上预测池和交易风险回答。"
                        + "如果用户提供 App 页面行情或链上池子数据，必须以这些数据为准，不要编造其他实时价格。"
                        + "输出中文，简洁、可操作，并明确风险提示。",
                question,
                new DeepSeekClient.ChatCallback() {
                    @Override
                    public void onSuccess(String response) {
                        callback.onGeneralAdvice(question, response);
                    }

                    @Override
                    public void onError(String error) {
                        callback.onError(error);
                    }
                });
    }

    private static boolean containsPrivateKey(String text) {
        if (text == null) return false;
        // 64 hex chars (private key) or 12/24 word seed phrase
        String stripped = text.replaceAll("\\s+", "");
        if (stripped.matches("(?i)^[0-9a-f]{64}$")) return true;
        int wordCount = text.trim().split("\\s+").length;
        return wordCount == 12 || wordCount == 24;
    }

    // =============== 工具方法 ===============

    public ShardProfit[] parseShardProfitsPublic(String json) {
        return parseShardProfits(json);
    }

    private ShardProfit[] parseShardProfits(String json) {
        try {
            Type mapType = new TypeToken<Map<String, String>>(){}.getType();
            Map<String, String> map = gson.fromJson(json, mapType);

            ShardProfit[] shards = new ShardProfit[map.size()];
            int i = 0;
            for (Map.Entry<String, String> e : map.entrySet()) {
                ShardProfit s = new ShardProfit();
                s.shardIndex = Integer.parseInt(e.getKey());
                String[] parts = e.getValue().split("/");
                if (parts.length >= 2) {
                    s.profit = parseDouble(parts[0]);
                    s.staked = parseDouble(parts[1]);
                }
                s.yieldPct = s.staked > 0 ? s.profit / s.staked : 0;
                shards[i++] = s;
            }
            return shards;
        } catch (Exception e) {
            Log.e("AgentManager", "Failed to parse broker profit data: " + json, e);
            return new ShardProfit[0];
        }
    }

    private double parseDouble(String s) {
        try { return Double.parseDouble(s.trim()); }
        catch (NumberFormatException e) { return 0; }
    }

    // =============== 数据模型 ===============

    public static class ShardProfit {
        public int shardIndex;
        public double profit;
        public double staked;
        public double yieldPct;
    }

    public static class BrokerReport {
        public String rawAnalysis;
        public ShardProfit[] shards;
    }

    public interface AnalysisCallback {
        void onBrokerReport(BrokerReport report);
        void onGeneralAdvice(String question, String answer);
        void onError(String error);
    }
}
