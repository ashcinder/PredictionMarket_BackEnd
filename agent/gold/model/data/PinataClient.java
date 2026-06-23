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
    public static final String IPFS_GATEWAY = "http://10.0.2.2:8080/ipfs/";
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
        return uploadFileToIPFS(jsonMetadata.toString().getBytes(StandardCharsets.UTF_8), "metadata.json", "application/json");
    }

    /**
     * 将字节数组上传到本地 IPFS 节点
     */
    public static String uploadFileToIPFS(byte[] data, String fileName, String contentType) throws Exception {
        URL url = new URL(IPFS_API_ADD);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setDoOutput(true);

        String boundary = "IPFS_BOUNDARY_" + System.currentTimeMillis();
        conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=" + boundary);

        try (OutputStream os = conn.getOutputStream()) {
            PrintWriter writer = new PrintWriter(new OutputStreamWriter(os, StandardCharsets.UTF_8), true);
            writer.append("--").append(boundary).append("\r\n");
            writer.append("Content-Disposition: form-data; name=\"file\"; filename=\"").append(fileName).append("\"\r\n");
            writer.append("Content-Type: ").append(contentType).append("\r\n\r\n");
            writer.flush();
            os.write(data);
            os.flush();
            writer.append("\r\n");
            writer.append("--").append(boundary).append("--\r\n");
            writer.flush();
        }

        int responseCode = conn.getResponseCode();
        if (responseCode != 200) {
            throw new Exception("IPFS 上传失败: HTTP " + responseCode);
        }

        try (Scanner scanner = new Scanner(conn.getInputStream(), "UTF-8")) {
            String response = scanner.useDelimiter("\\A").next();
            JSONObject resJson = new JSONObject(response);
            return resJson.getString("Hash");
        }
    }
   /**
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