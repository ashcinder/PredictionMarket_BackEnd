package com.example.brokerfi.xc.agent.gold.model.data;

import android.util.Log;

import org.json.JSONObject;

import java.io.OutputStream;
import java.io.OutputStreamWriter;
import java.io.PrintWriter;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Scanner;

/**
 * IPFS 存储服务客户端 (已切换为 Mac 本地 IPFS 节点模式)
 */
public class PinataClient {
    private static final String TAG = "LocalIPFSClient";

    // ---------------------------------------------------------
    // IP 配置说明：
    // 1. Android Studio 模拟器访问 Mac 宿主机，固定使用 10.0.2.2
    // 2. 如果你是真机调试，请修改为 Mac 的局域网 IP (如 http://192.168.1.100:8080/ipfs/)
    // ---------------------------------------------------------
    private static final String IPFS_GATEWAY = "http://10.0.2.2:8080/ipfs/";
    private static final String IPFS_API_ADD = "http://10.0.2.2:5001/api/v0/add";

    /**
     * 将 JSON 元数据上传到本地 IPFS 节点
     * @return 返回文件的 CID (哈希值)
     */
    /**
     * 将 JSON 元数据上传到本地 IPFS 节点
     * @return 返回文件的 CID (哈希值)
     */
    /**
     * 将 JSON 元数据上传到本地 IPFS 节点 (带全链路调试日志)
     * @return 返回文件的 CID (哈希值)
     */
    public static String uploadJsonToIPFS(JSONObject jsonMetadata) throws Exception {
        URL url = new URL(IPFS_API_ADD);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setDoOutput(true);

        // 使用更标准、没有特殊字符的 boundary
        String boundary = "IPFS_BOUNDARY_" + System.currentTimeMillis();
        conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);

        // 将整个请求体先拼装成一个完整的字符串，方便打印和校验
        String jsonStr = jsonMetadata.toString();
        StringBuilder payloadBuilder = new StringBuilder();
        payloadBuilder.append("--").append(boundary).append("\r\n");
        // 注意这里的 name 必须是 "file" (部分 IPFS 版本要求带引号)
        payloadBuilder.append("Content-Disposition: form-data; name=\"file\"; filename=\"metadata.json\"\r\n");
        payloadBuilder.append("Content-Type: application/json\r\n\r\n");
        payloadBuilder.append(jsonStr).append("\r\n"); // JSON 数据末尾加上换行，防止与边界符粘连
        payloadBuilder.append("--").append(boundary).append("--\r\n");

        String payload = payloadBuilder.toString();

        // ----------------- 开启极限调试日志 -----------------
        android.util.Log.d(TAG, "==== IPFS 极客调试: 上传报文 ====");
        android.util.Log.d(TAG, "URL: " + url.toString());
        android.util.Log.d(TAG, "Content-Type: multipart/form-data; boundary=" + boundary);
        android.util.Log.d(TAG, "Payload Length: " + payload.getBytes(StandardCharsets.UTF_8).length);
        android.util.Log.d(TAG, "Payload Content (严格核对空行):\n" + payload);
        android.util.Log.d(TAG, "===================================");
        // ----------------------------------------------------

        // 一次性将完整的报文写入流
        try (OutputStream os = conn.getOutputStream()) {
            os.write(payload.getBytes(StandardCharsets.UTF_8));
            os.flush();
        }

        int responseCode = conn.getResponseCode();
        android.util.Log.d(TAG, "响应状态码 (Response Code): " + responseCode);
        android.util.Log.d(TAG, "响应信息 (Response Message): " + conn.getResponseMessage());

        if (responseCode != 200) {
            java.io.InputStream errorStream = conn.getErrorStream();
            String errorMsg = "";
            if (errorStream != null) {
                try (Scanner scanner = new Scanner(errorStream, "UTF-8")) {
                    errorMsg = scanner.useDelimiter("\\A").hasNext() ? scanner.useDelimiter("\\A").next() : "";
                }
            }
            android.util.Log.e(TAG, "IPFS 原生报错详情: " + errorMsg);
            throw new Exception("HTTP " + responseCode + " - " + errorMsg.trim());
        }

        try (Scanner scanner = new Scanner(conn.getInputStream(), "UTF-8")) {
            String response = scanner.useDelimiter("\\A").next();
            android.util.Log.d(TAG, "IPFS 上传成功, 原始响应: " + response);
            JSONObject resJson = new JSONObject(response);

            // 本地节点返回的 CID 键名为 "Hash"
            String cid = resJson.getString("Hash");
            android.util.Log.d(TAG, "解析获得的 CID: " + cid);
            return cid;
        }
    }    /**
     * 从本地 IPFS 节点网关下载 JSON 文本
     */
    public static String downloadJsonFromIPFS(String cid) throws Exception {
        if (cid == null || cid.isEmpty()) return "{}";

        URL url = new URL(IPFS_GATEWAY + cid);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("GET");

        // 局域网内请求速度极快，超时时间可以设置得更短
        conn.setConnectTimeout(5000);
        conn.setReadTimeout(5000);

        if (conn.getResponseCode() != 200) {
            throw new Exception("本地 IPFS 读取失败: HTTP " + conn.getResponseCode());
        }

        try (Scanner scanner = new Scanner(conn.getInputStream(), "UTF-8")) {
            String result = scanner.useDelimiter("\\A").next();
            Log.d(TAG, "本地节点读取成功, CID: " + cid);
            return result;
        }
    }
}