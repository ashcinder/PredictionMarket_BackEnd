package com.example.brokerfi.xc.agent.gold.model.logic;

import com.example.brokerfi.xc.agent.model.DeepSeekClient;
import com.example.brokerfi.xc.agent.gold.model.data.AppExecutors;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.ArrayList;
import java.util.Calendar;
import java.util.List;
import java.util.Scanner;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicReference;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public class GoldAdvisoryManager {

    private static double sinaPrevClose = 0;
    private static boolean sinaPrevCloseFetched = false;

    public static class Advisory {
        public String signal = "HOLD";
        public int confidence = 50;
        public double priceUsd = 0;
        public double change24h = 0;
        public double usdCny = 0;
        public String quoteSource = "";
        public String quoteUpdatedAt = "";
        public boolean quoteDelayed = false;
        public String summary = "";
        public List<String> factors = new ArrayList<>();
    }

    public interface AdvisoryCallback {
        void onSuccess(Advisory advisory);
        void onError(String error);
    }

    public static void fetch(AdvisoryCallback callback) {
        if (!DeepSeekClient.isConfigured()) {
            AppExecutors.getInstance().mainThread().execute(() -> callback.onError("NO_API_KEY"));
            return;
        }
        AppExecutors.getInstance().networkIO().execute(() -> {
            AtomicReference<Advisory> goldQuote = new AtomicReference<>(emptyQuote());
            AtomicReference<Double> usdCny = new AtomicReference<>(0.0);
            CountDownLatch latch = new CountDownLatch(2);

            AppExecutors.getInstance().networkIO().execute(() -> {
                try { goldQuote.set(fetchGoldQuote()); } finally { latch.countDown(); }
            });
            AppExecutors.getInstance().networkIO().execute(() -> {
                try { usdCny.set(fetchUsdCny()); } finally { latch.countDown(); }
            });

            try { latch.await(); } catch (InterruptedException ignored) {}

            Advisory quote = goldQuote.get();
            double price = quote.priceUsd;
            double change = quote.change24h;
            double cny = usdCny.get();

            String systemPrompt = buildSystemPrompt();
            String userMessage = buildUserPrompt(quote, cny);

            DeepSeekClient.chat(systemPrompt, userMessage, new DeepSeekClient.ChatCallback() {
                @Override
                public void onSuccess(String reply) {
                    Advisory a = parseAdvisory(reply, price, change, cny);
                    copyQuoteMeta(quote, a);
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(a));
                }
                @Override
                public void onError(String error) {
                    AppExecutors.getInstance().mainThread().execute(() -> callback.onError(error));
                }
            });
        });
    }

    public static void fetchPrice(AdvisoryCallback callback) {
        AppExecutors.getInstance().networkIO().execute(() -> {
            try {
                Advisory quote = fetchGoldQuote();
                AppExecutors.getInstance().mainThread().execute(() -> callback.onSuccess(quote));
            } catch (Exception e) {
                AppExecutors.getInstance().mainThread().execute(() -> callback.onError(e.getMessage()));
            }
        });
    }

    // Gold price: gold-api.com with sina fallback

    static Advisory fetchGoldQuote() {
        try {
            URL url = new URL("https://api.gold-api.com/price/XAU");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(5000);
            conn.setReadTimeout(5000);
            if (conn.getResponseCode() == 200) {
                try (InputStream is = conn.getInputStream();
                     Scanner sc = new Scanner(is, "UTF-8").useDelimiter("\\A")) {
                    String body = sc.hasNext() ? sc.next() : "";
                    JSONObject json = new JSONObject(body);
                    double price = json.optDouble("price", 0);
                    if (price > 0) {
                        double prevClose = getSinaPrevClose();
                        double change = prevClose > 0 ? (price - prevClose) / prevClose * 100 : 0;
                        Advisory quote = emptyQuote();
                        quote.priceUsd = price;
                        quote.change24h = change;
                        quote.quoteSource = "gold-api.com";
                        quote.quoteUpdatedAt = json.optString("updatedAtReadable",
                                json.optString("updatedAt", ""));
                        quote.quoteDelayed = isWeekendNow();
                        return quote;
                    }
                }
            }
        } catch (Exception ignored) {}
        return fetchGoldSina();
    }

    private static synchronized double getSinaPrevClose() {
        if (sinaPrevCloseFetched) return sinaPrevClose;
        sinaPrevCloseFetched = true;
        try {
            URL url = new URL("https://hq.sinajs.cn/list=hf_XAU");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(5000); conn.setReadTimeout(5000);
            conn.setRequestProperty("Referer", "https://finance.sina.com.cn");
            conn.setRequestProperty("User-Agent", "Mozilla/5.0 (Linux; Android 12)");
            if (conn.getResponseCode() == 200) {
                try (InputStream is = conn.getInputStream();
                     Scanner sc = new Scanner(is, "GBK").useDelimiter("\\A")) {
                    String body = sc.hasNext() ? sc.next() : "";
                    int start = body.indexOf('"');
                    int end = body.lastIndexOf('"');
                    if (start >= 0 && end > start) {
                        String[] fields = body.substring(start + 1, end).split(",");
                        if (fields.length > 1) sinaPrevClose = Double.parseDouble(fields[1]);
                    }
                }
            }
        } catch (Exception ignored) {}
        return sinaPrevClose;
    }

    private static Advisory fetchGoldSina() {
        try {
            URL url = new URL("https://hq.sinajs.cn/list=hf_XAU");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(5000); conn.setReadTimeout(5000);
            conn.setRequestProperty("Referer", "https://finance.sina.com.cn");
            conn.setRequestProperty("User-Agent", "Mozilla/5.0 (Linux; Android 12)");
            if (conn.getResponseCode() == 200) {
                try (InputStream is = conn.getInputStream();
                     Scanner sc = new Scanner(is, "GBK").useDelimiter("\\A")) {
                    String body = sc.hasNext() ? sc.next() : "";
                    int start = body.indexOf('"');
                    int end = body.lastIndexOf('"');
                    if (start >= 0 && end > start) {
                        String[] fields = body.substring(start + 1, end).split(",");
                        if (fields.length > 1) {
                            double price = Double.parseDouble(fields[0]);
                            double prevClose = Double.parseDouble(fields[1]);
                            double change = (prevClose > 0) ? (price - prevClose) / prevClose * 100 : 0;
                            if (price > 0) {
                                Advisory quote = emptyQuote();
                                quote.priceUsd = price;
                                quote.change24h = change;
                                quote.quoteSource = "新浪财经";
                                quote.quoteUpdatedAt = fields.length > 12
                                        ? fields[12] + " " + fields[6]
                                        : fields.length > 6 ? fields[6] : "";
                                quote.quoteDelayed = true;
                                return quote;
                            }
                        }
                    }
                }
            }
        } catch (Exception ignored) {}
        return emptyQuote();
    }

    static double fetchUsdCny() {
        try {
            URL url = new URL("https://open.er-api.com/v6/latest/USD");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(5000); conn.setReadTimeout(5000);
            if (conn.getResponseCode() == 200) {
                try (InputStream is = conn.getInputStream();
                     Scanner sc = new Scanner(is, "UTF-8").useDelimiter("\\A")) {
                    String body = sc.hasNext() ? sc.next() : "";
                    return new JSONObject(body).getJSONObject("rates").optDouble("CNY", 0);
                }
            }
        } catch (Exception ignored) {}
        return 0;
    }

    private static String buildSystemPrompt() {
        return "你是一位专业的黄金市场分析师，服务于散户投资者。" +
                "请综合多个维度进行分析，给出简明投资建议。" +
                "如果用户提供App页面行情或链上预测池数据，必须以这些数据为准，不要编造其他实时价格。" +
                "严格按要求的JSON格式输出，不要用代码块包裹。";
    }

    private static String buildUserPrompt(Advisory quote, double cny) {
        String priceInfo = quote.priceUsd > 0
                ? String.format("当前黄金现货价格 $%.2f/盎司，24小时涨跌幅 %+.2f%%，来源 %s，更新时间 %s",
                        quote.priceUsd, quote.change24h, quote.quoteSource, quote.quoteUpdatedAt)
                : "（实时金价暂时获取失败）";
        String cnyInfo = cny > 0
                ? String.format("当前美元兑人民币汇率 %.4f", cny)
                : "（汇率暂时获取失败）";

        return "【实时行情】\n" +
                "• " + priceInfo + "\n" +
                "• " + cnyInfo + "\n\n" +
                "请综合以下维度进行分析，给出简明投资建议：\n" +
                "1. 当前价位与近期走势研判\n" +
                "2. 地缘政治对黄金避险需求的影响\n" +
                "3. 美元走势与通胀预期\n" +
                "4. 央行购金动态与机构资金流向\n\n" +
                "严格按此 JSON 格式输出（不要用代码块包裹）：\n" +
                "GOLD_ADVISORY:{\"signal\":\"BUY或HOLD或SELL\",\"confidence\":置信度0到100," +
                "\"priceUsd\":" + (quote.priceUsd > 0 ? quote.priceUsd : 0) + "," +
                "\"summary\":\"一句话核心结论（中文，30字以内）\"," +
                "\"factors\":[\"因素1\",\"因素2\",\"因素3\"]}";
    }

    private static Advisory parseAdvisory(String reply, double price, double change, double cny) {
        Advisory a = new Advisory();
        a.priceUsd = price;
        a.change24h = change;
        a.usdCny = cny;
        try {
            Pattern p = Pattern.compile("GOLD_ADVISORY:(\\{.*\\})", Pattern.DOTALL);
            Matcher m = p.matcher(reply);
            if (m.find()) {
                JSONObject json = new JSONObject(m.group(1));
                a.signal = json.optString("signal", "HOLD").toUpperCase();
                a.confidence = Math.max(0, Math.min(100, json.optInt("confidence", 50)));
                a.summary = json.optString("summary", "");
                JSONArray arr = json.optJSONArray("factors");
                if (arr != null) {
                    for (int i = 0; i < arr.length(); i++) a.factors.add(arr.getString(i));
                }
            } else {
                String upper = reply.toUpperCase();
                if (upper.contains("BUY") || upper.contains("买入") || upper.contains("看多")) a.signal = "BUY";
                else if (upper.contains("SELL") || upper.contains("卖出") || upper.contains("看空")) a.signal = "SELL";
                a.summary = reply.length() > 80 ? reply.substring(0, 80) + "…" : reply;
            }
        } catch (Exception e) {
            a.summary = "结果解析异常，请重试";
        }
        return a;
    }

    private static Advisory emptyQuote() {
        Advisory quote = new Advisory();
        quote.quoteSource = "";
        quote.quoteUpdatedAt = "";
        quote.quoteDelayed = true;
        return quote;
    }

    private static void copyQuoteMeta(Advisory from, Advisory to) {
        to.quoteSource = from.quoteSource;
        to.quoteUpdatedAt = from.quoteUpdatedAt;
        to.quoteDelayed = from.quoteDelayed;
    }

    private static boolean isWeekendNow() {
        int dayOfWeek = Calendar.getInstance().get(Calendar.DAY_OF_WEEK);
        return dayOfWeek == Calendar.SATURDAY || dayOfWeek == Calendar.SUNDAY;
    }
}
