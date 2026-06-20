package com.example.brokerfi.xc.agent.ai;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;

import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Scanner;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public class DeepSeekClient {

    private static final String TAG = "DeepSeekClient";
    private static final String API_URL = "https://api.deepseek.com/chat/completions";
    private static final String PREFS_NAME = "deepseek_prefs";
    private static final String KEY_API_KEY = "api_key";
    private static final int CONNECT_TIMEOUT_MS = 15000;
    private static final int READ_TIMEOUT_MS = 45000;

    private static Context appContext;
    private static final Gson gson = new Gson();
    private static final ExecutorService executor = Executors.newSingleThreadExecutor();

    public static void init(Context context) {
        appContext = context.getApplicationContext();
    }

    public static boolean isConfigured() {
        if (appContext == null) return false;
        return isValidApiKey(getApiKey());
    }

    public static String getApiKey() {
        if (appContext == null) return null;
        String key = appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getString(KEY_API_KEY, null);
        return key == null ? null : key.trim();
    }

    public static boolean setApiKey(String key) {
        if (appContext == null) return false;
        appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putString(KEY_API_KEY, key).apply();
        return true;
    }

    public interface ChatCallback {
        void onSuccess(String response);
        void onError(String error);
    }

    public static void chat(String systemPrompt, String userMessage, ChatCallback callback) {
        if (callback == null) return;
        String apiKey = getApiKey();
        if (!isValidApiKey(apiKey)) {
            callback.onError("NO_API_KEY");
            return;
        }

        executor.execute(() -> {
            HttpURLConnection conn = null;
            try {
                List<Message> messages = new ArrayList<>();
                messages.add(new Message("system", systemPrompt));
                messages.add(new Message("user", userMessage));

                ChatRequest request = new ChatRequest();
                request.model = "deepseek-chat";
                request.messages = messages;
                request.temperature = 0.7;
                request.maxTokens = 1024;

                String json = gson.toJson(request);

                URL url = new URL(API_URL);
                conn = (HttpURLConnection) url.openConnection();
                conn.setRequestMethod("POST");
                conn.setRequestProperty("Accept", "application/json");
                conn.setRequestProperty("Content-Type", "application/json; charset=UTF-8");
                conn.setRequestProperty("Authorization", "Bearer " + apiKey);
                conn.setDoOutput(true);
                conn.setConnectTimeout(CONNECT_TIMEOUT_MS);
                conn.setReadTimeout(READ_TIMEOUT_MS);

                try (OutputStream os = conn.getOutputStream()) {
                    os.write(json.getBytes(StandardCharsets.UTF_8));
                }

                int code = conn.getResponseCode();
                String body = readStream(code >= 200 && code < 300
                        ? conn.getInputStream()
                        : conn.getErrorStream());
                if (code >= 200 && code < 300) {
                    ChatResponse response = gson.fromJson(body, ChatResponse.class);
                    String content = extractContent(response);
                    callback.onSuccess(content);
                } else {
                    String error = buildHttpError(code, body);
                    Log.w(TAG, error);
                    callback.onError(error);
                }
            } catch (Exception e) {
                Log.w(TAG, "DeepSeek request failed", e);
                callback.onError(e.getClass().getSimpleName() + ": " + e.getMessage());
            } finally {
                if (conn != null) {
                    conn.disconnect();
                }
            }
        });
    }

    public static void chatSimple(String userMessage, ChatCallback callback) {
        chat("你是一个专业的区块链DeFi助手，服务于BrokerChain钱包用户。请用中文回答，简洁、可操作。",
                userMessage, callback);
    }

    private static boolean isValidApiKey(String key) {
        return key != null && key.startsWith("sk-") && key.length() > 10;
    }

    private static String readStream(InputStream stream) {
        if (stream == null) return "";
        try (Scanner scanner = new Scanner(stream, "UTF-8").useDelimiter("\\A")) {
            return scanner.hasNext() ? scanner.next() : "";
        }
    }

    private static String extractContent(ChatResponse response) {
        if (response == null || response.choices == null || response.choices.isEmpty()
                || response.choices.get(0) == null
                || response.choices.get(0).message == null
                || response.choices.get(0).message.content == null
                || response.choices.get(0).message.content.trim().isEmpty()) {
            throw new IllegalStateException("DeepSeek returned an empty response");
        }
        return response.choices.get(0).message.content.trim();
    }

    private static String buildHttpError(int code, String body) {
        String detail = body == null || body.trim().isEmpty()
                ? "no response body"
                : body.trim();
        if (detail.length() > 300) {
            detail = detail.substring(0, 300) + "...";
        }
        return "HTTP " + code + ": " + detail;
    }

    static class ChatRequest {
        String model;
        List<Message> messages;
        double temperature;
        @SerializedName("max_tokens")
        int maxTokens;
    }

    static class Message {
        String role;
        String content;

        Message(String role, String content) {
            this.role = role;
            this.content = content;
        }
    }

    static class ChatResponse {
        List<Choice> choices;
    }

    static class Choice {
        Message message;
    }
}
