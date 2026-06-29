package com.example.brokerfi.xc.agent.gold.view;

import android.content.Intent;
import android.os.Bundle;
import android.util.Log;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.EditText;
import android.widget.GridLayout;
import android.widget.ImageView;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.appcompat.widget.AppCompatButton;
import androidx.fragment.app.Fragment;

import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.ai.DeepSeekClient;

import org.json.JSONObject;

import java.text.SimpleDateFormat;
import java.util.Arrays;
import java.util.Date;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

public class GoldCreatePoolFragment extends Fragment {

    private EditText etAiInput;
    private AppCompatButton btnAiAnalyze;
    private final SimpleDateFormat dateFormat = new SimpleDateFormat("yyyy-MM-dd", Locale.getDefault());

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        View view = inflater.inflate(R.layout.fragment_gold_create_pool, container, false);
        etAiInput = view.findViewById(R.id.et_ai_input);
        btnAiAnalyze = view.findViewById(R.id.btn_ai_analyze);
        
        btnAiAnalyze.setOnClickListener(v -> performAiAnalysis());
        
        initTemplates(view.findViewById(R.id.template_grid));
        return view;
    }

    private void performAiAnalysis() {
        String input = etAiInput.getText().toString().trim();
        if (input.isEmpty()) {
            Toast.makeText(getContext(), "Please describe your idea / 请输入您的创意", Toast.LENGTH_SHORT).show();
            return;
        }

        btnAiAnalyze.setEnabled(false);
        btnAiAnalyze.setText("✨ AI Parsing Across Languages...");

        String today = dateFormat.format(new Date());
        String systemPrompt =
                "You are a precise configuration parser for a Gold Prediction Market. " +
                "Your ONLY job: map a user's gold-price bet description to EXACTLY ONE of 6 templates below. " +
                "You MUST output pure JSON — no markdown, no explanations, no code fences.\n\n" +

                "=== CRITICAL RULES ===\n" +
                "1. Input MUST describe a concrete, testable prediction about gold (price/volume/indicator).\n" +
                "2. If the input does NOT clearly match any template → set confidence ≤ 0.3.\n" +
                "3. If ambiguous between 2+ templates → pick the BEST match but set confidence ≤ 0.5.\n" +
                "4. Extract exact numbers, dates, directions from input. daysFromNow: calculate from today " + today + ".\n" +
                "5. If no deadline mentioned, default daysFromNow = 7.\n\n" +

                "=== TEMPLATE 1: TYPE_PRICE — Directional prediction ===\n" +
                "USE WHEN: User predicts gold price UP/DOWN/FLAT over a period. Focus is DIRECTION, NOT a specific price number.\n" +
                "TRIGGERS: 涨/跌/横盘/up/down/flat/rise/fall/bullish/bearish  WITHOUT a specific USD price target.\n" +
                "NOT FOR: Specific prices like $2500 → TYPE_TOUCH. Percentages → TYPE_VOLATILITY.\n" +
                "REQUIRED: directionIdx (0=Up, 1=Down, 2=Flat). param1 = \"\" (empty).\n" +
                "EXAMPLES:\n" +
                "  「下周黄金会涨吗?」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「我赌周五金价下跌」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":1,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":5,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「Gold will drop by Friday」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":1,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":5,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「下周横盘整理」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":2,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n\n" +

                "=== TEMPLATE 2: TYPE_VOLATILITY — Price fluctuation magnitude ===\n" +
                "USE WHEN: User bets on whether price CHANGE MAGNITUDE exceeds a PERCENTAGE threshold. Focus is VOLATILITY/AMPLITUDE.\n" +
                "TRIGGERS: 波动/volatility/振幅/涨跌幅/fluctuation/剧烈/平稳 + a percentage number.\n" +
                "NOT FOR: Direction-only (→ TYPE_PRICE). Specific price $X (→ TYPE_TOUCH).\n" +
                "REQUIRED: param1 = volatility percentage (just the number, no % sign).\n" +
                "EXAMPLES:\n" +
                "  「下周金价波动会超过3%吗?」 → {\"type\":\"TYPE_VOLATILITY\",\"param1\":\"3\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「本周行情很平稳，波动不超1%」 → {\"type\":\"TYPE_VOLATILITY\",\"param1\":\"1\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n" +
                "  「Will gold volatility exceed 5% this month?」 → {\"type\":\"TYPE_VOLATILITY\",\"param1\":\"5\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":30,\"liquidity\":100,\"confidence\":0.95}\n\n" +

                "=== TEMPLATE 3: TYPE_VOLUME — Trading volume prediction ===\n" +
                "USE WHEN: User bets on whether trading VOLUME exceeds/falls below a threshold (tons) on a specific DAY.\n" +
                "TRIGGERS: 成交量/volume/交易量/吨/tons + a numeric threshold.\n" +
                "REQUIRED: param1 = volume in tons (number). operatorIdx: 0=Above/大于, 1=Below/小于, 2=Equal/等于.\n" +
                "EXAMPLES:\n" +
                "  「周五成交量会超过500吨吗?」 → {\"type\":\"TYPE_VOLUME\",\"param1\":\"500\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":5,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「明天交易量低于300吨」 → {\"type\":\"TYPE_VOLUME\",\"param1\":\"300\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":1,\"daysFromNow\":1,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「Will volume be above 800 tons this Friday?」 → {\"type\":\"TYPE_VOLUME\",\"param1\":\"800\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":5,\"liquidity\":100,\"confidence\":0.95}\n\n" +

                "=== TEMPLATE 4: TYPE_TECHNICAL — Technical indicator condition ===\n" +
                "USE WHEN: User bets on a TECHNICAL INDICATOR (RSI/MACD/KDJ/BOLL) reaching a condition.\n" +
                "TRIGGERS: RSI/MACD/KDJ/布林带/BOLL/金叉/死叉/超买/超卖/technical indicator name.\n" +
                "indicatorIdx: 0=RSI, 1=MACD, 2=KDJ, 3=BOLL.\n" +
                "operatorIdx: 0=Above/大于, 1=Below/小于, 2=CrossUp/金叉, 3=CrossDown/死叉.\n" +
                "REQUIRED: param1 = threshold value (0 for pure cross signals like 金叉/死叉). indicatorIdx must be correct.\n" +
                "EXAMPLES:\n" +
                "  「RSI会超过70进入超买区吗?」 → {\"type\":\"TYPE_TECHNICAL\",\"param1\":\"70\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「MACD会形成金叉吗?」 → {\"type\":\"TYPE_TECHNICAL\",\"param1\":\"0\",\"directionIdx\":0,\"indicatorIdx\":1,\"operatorIdx\":2,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n" +
                "  「KDJ死叉」 → {\"type\":\"TYPE_TECHNICAL\",\"param1\":\"0\",\"directionIdx\":0,\"indicatorIdx\":2,\"operatorIdx\":3,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n" +
                "  「布林带下轨会被跌破吗?」 → {\"type\":\"TYPE_TECHNICAL\",\"param1\":\"0\",\"directionIdx\":0,\"indicatorIdx\":3,\"operatorIdx\":1,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.85}\n" +
                "  「Will MACD cross above signal line?」 → {\"type\":\"TYPE_TECHNICAL\",\"param1\":\"0\",\"directionIdx\":0,\"indicatorIdx\":1,\"operatorIdx\":2,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n\n" +

                "=== TEMPLATE 5: TYPE_TOUCH — Price level touch ===\n" +
                "USE WHEN: User bets on whether gold will TOUCH/REACH/HIT a specific USD price level within a period.\n" +
                "TRIGGERS: 触及/touch/reach/hit/触碰/达到/突破 + a specific PRICE (e.g. $2500, 3000美元).\n" +
                "KEY: Has a CONCRETE USD PRICE NUMBER as the target. Not about direction — about whether that level is reached.\n" +
                "NOT FOR: Direction without a price (→ TYPE_PRICE). Percentages (→ TYPE_VOLATILITY).\n" +
                "REQUIRED: param1 = target price in USD (number only, no $ sign).\n" +
                "EXAMPLES:\n" +
                "  「金价本周会触及2500美元吗?」 → {\"type\":\"TYPE_TOUCH\",\"param1\":\"2500\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「黄金能突破3000吗?」 → {\"type\":\"TYPE_TOUCH\",\"param1\":\"3000\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n" +
                "  「Will gold touch $2800 by end of month?」 → {\"type\":\"TYPE_TOUCH\",\"param1\":\"2800\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":30,\"liquidity\":100,\"confidence\":0.95}\n\n" +

                "=== TEMPLATE 6: TYPE_RELATIVE — Gold vs other assets ===\n" +
                "USE WHEN: User compares gold's RETURN/PERFORMANCE against another asset (BTC, stocks, silver, etc.).\n" +
                "TRIGGERS: 跑赢/outperform/vs/对比/相比 + another asset name (BTC/比特币/股票/白银/S&P).\n" +
                "REQUIRED: param1 = comparison asset name (e.g. \"BTC\", \"S&P 500\", \"白银\").\n" +
                "EXAMPLES:\n" +
                "  「黄金能跑赢BTC吗?」 → {\"type\":\"TYPE_RELATIVE\",\"param1\":\"BTC\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.95}\n" +
                "  「黄金vs比特币谁更强?」 → {\"type\":\"TYPE_RELATIVE\",\"param1\":\"BTC\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.90}\n" +
                "  「Will gold outperform the S&P 500 this quarter?」 → {\"type\":\"TYPE_RELATIVE\",\"param1\":\"S&P 500\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":90,\"liquidity\":100,\"confidence\":0.95}\n\n" +

                "=== REJECTION — When NO template fits (confidence ≤ 0.3) ===\n" +
                "Set confidence ≤ 0.3 for: NOT about gold/finance; research/analysis not a bet; too vague; impossible to map.\n" +
                "REJECTION EXAMPLES:\n" +
                "  「今天天气真好」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.0}\n" +
                "  「帮我分析一下最近的黄金行情」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.2}\n" +
                "  「我想赚钱」 → {\"type\":\"TYPE_PRICE\",\"param1\":\"\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.1}\n\n" +

                "=== OUTPUT ===\n" +
                "Output ONLY: {\"type\":\"TYPE_...\",\"param1\":\"...\",\"directionIdx\":0,\"indicatorIdx\":0,\"operatorIdx\":0,\"daysFromNow\":7,\"liquidity\":100,\"confidence\":0.0}";

        DeepSeekClient.chatForParsing(systemPrompt, input, new DeepSeekClient.ChatCallback() {
            @Override
            public void onSuccess(String response) {
                if (getActivity() == null) return;
                getActivity().runOnUiThread(() -> {
                    btnAiAnalyze.setEnabled(true);
                    btnAiAnalyze.setText("智能解析并进入配置");
                    handleAiResponse(response);
                });
            }

            @Override
            public void onError(String error) {
                if (getActivity() == null) return;
                getActivity().runOnUiThread(() -> {
                    btnAiAnalyze.setEnabled(true);
                    btnAiAnalyze.setText("智能解析并进入配置");
                    Toast.makeText(getContext(), "AI Error: " + error, Toast.LENGTH_SHORT).show();
                });
            }
        });
    }

    private static final Set<String> VALID_TYPES = new HashSet<>(Arrays.asList(
            "TYPE_PRICE", "TYPE_VOLATILITY", "TYPE_VOLUME",
            "TYPE_TECHNICAL", "TYPE_TOUCH", "TYPE_RELATIVE"));

    private static final Map<String, String> TYPE_TITLE_MAP = new HashMap<>();
    static {
        TYPE_TITLE_MAP.put("TYPE_PRICE", "价格涨跌博弈");
        TYPE_TITLE_MAP.put("TYPE_VOLATILITY", "波动幅度博弈");
        TYPE_TITLE_MAP.put("TYPE_VOLUME", "交易量博弈");
        TYPE_TITLE_MAP.put("TYPE_TECHNICAL", "技术指标博弈");
        TYPE_TITLE_MAP.put("TYPE_TOUCH", "极值触碰博弈");
        TYPE_TITLE_MAP.put("TYPE_RELATIVE", "跑赢率博弈");
    }

    private static final double CONFIDENCE_THRESHOLD = 0.7;

    private void handleAiResponse(String response) {
        try {
            // 1. Robust JSON extraction — handles markdown code blocks and surrounding text
            String jsonStr = extractJson(response);
            JSONObject json = new JSONObject(jsonStr);

            // 2. Validate type is one of the 6 valid templates
            String type = json.optString("type", "");
            if (!VALID_TYPES.contains(type)) {
                Toast.makeText(getContext(),
                        "AI 未能匹配到合适的博弈模版，请尝试更具体的描述或手动选择模版",
                        Toast.LENGTH_LONG).show();
                return;
            }

            // 3. Validate confidence meets threshold
            double confidence = json.optDouble("confidence", 0.0);
            if (confidence < CONFIDENCE_THRESHOLD) {
                String hint = confidence < 0.3
                        ? "您的描述似乎与黄金博弈无关，请描述一个具体的黄金预测"
                        : "AI 对解析结果信心不足，请尝试用更明确的语言描述您的博弈创意";
                Toast.makeText(getContext(),
                        "解析置信度过低 (" + String.format("%.0f%%", confidence * 100) + ")\n" + hint,
                        Toast.LENGTH_LONG).show();
                return;
            }

            // 4. Template-specific field validation
            String validationError = validateTemplateFields(type, json);
            if (validationError != null) {
                Toast.makeText(getContext(), validationError, Toast.LENGTH_LONG).show();
                return;
            }

            // 5. Success — launch configuration activity with AI-parsed data
            String title = TYPE_TITLE_MAP.get(type);

            Intent intent = new Intent(requireContext(), GoldCreateCustomActivity.class);
            intent.putExtra("TEMPLATE_TYPE", type);
            intent.putExtra("TEMPLATE_TITLE", title);
            intent.putExtra("AI_PARSED_DATA", jsonStr);
            startActivity(intent);

            etAiInput.setText("");
        } catch (Exception e) {
            Log.e("AiResponse", "Parsing failed: " + response, e);
            Toast.makeText(getContext(), "AI 解析结果格式异常，请重试或手动选择模版", Toast.LENGTH_SHORT).show();
        }
    }

    /**
     * Extract JSON object from LLM response, handling markdown code blocks
     * (```json ... ```) and any surrounding explanatory text.
     */
    private String extractJson(String response) throws Exception {
        String cleaned = response.trim();

        // Strip markdown code fences if present
        int fenceStart = cleaned.indexOf("```");
        if (fenceStart >= 0) {
            int contentStart = cleaned.indexOf("\n", fenceStart);
            if (contentStart < 0) contentStart = fenceStart + 3;
            else contentStart = contentStart + 1;
            int fenceEnd = cleaned.lastIndexOf("```");
            if (fenceEnd > fenceStart) {
                cleaned = cleaned.substring(contentStart, fenceEnd).trim();
            }
        }

        // Locate the outermost JSON object
        int startIdx = cleaned.indexOf("{");
        int endIdx = cleaned.lastIndexOf("}");
        if (startIdx < 0 || endIdx < 0 || endIdx <= startIdx) {
            throw new Exception("No valid JSON object found in response");
        }
        return cleaned.substring(startIdx, endIdx + 1);
    }

    /**
     * Validate that required fields for each template type are present and plausible.
     * Returns an error message string if invalid, or null if the data is acceptable.
     */
    private String validateTemplateFields(String type, JSONObject json) {
        switch (type) {
            case "TYPE_PRICE": {
                int dir = json.optInt("directionIdx", -1);
                if (dir < 0 || dir > 2) {
                    return "AI 未正确识别价格方向（涨/跌/持平），请手动选择模版";
                }
                break;
            }
            case "TYPE_VOLATILITY": {
                String p1 = json.optString("param1", "");
                if (p1.isEmpty()) {
                    return "AI 未提取波动率百分比，请输入具体数值后重试";
                }
                break;
            }
            case "TYPE_VOLUME": {
                String p1 = json.optString("param1", "");
                if (p1.isEmpty()) {
                    return "AI 未提取目标成交量，请输入具体数值后重试";
                }
                int op = json.optInt("operatorIdx", -1);
                if (op < 0 || op > 2) {
                    return "AI 未正确识别成交量比较方式（大于/小于/等于）";
                }
                break;
            }
            case "TYPE_TECHNICAL": {
                int ind = json.optInt("indicatorIdx", -1);
                if (ind < 0 || ind > 3) {
                    return "AI 未正确识别技术指标类型（RSI/MACD/KDJ/BOLL），请手动选择";
                }
                // param1 can be "0" for pure cross signals like 金叉/死叉 — allowed
                break;
            }
            case "TYPE_TOUCH": {
                String p1 = json.optString("param1", "");
                if (p1.isEmpty()) {
                    return "AI 未提取目标触碰价格，请输入具体价格后重试";
                }
                break;
            }
            case "TYPE_RELATIVE": {
                String p1 = json.optString("param1", "");
                if (p1.isEmpty()) {
                    return "AI 未提取对比资产名称，请输入后重试";
                }
                break;
            }
        }
        return null;
    }

    private void initTemplates(GridLayout grid) {
        addTemplate(grid, "价格涨跌", "预测金价在某日涨跌", R.drawable.ic_template_price, "TYPE_PRICE");
        addTemplate(grid, "波动幅度", "预测行情剧烈程度", R.drawable.ic_template_volatility, "TYPE_VOLATILITY");
        addTemplate(grid, "交易量", "预测市场整体流动性", R.drawable.ic_template_volume, "TYPE_VOLUME");
        addTemplate(grid, "技术指标", "预测 RSI/MACD 形态", R.drawable.ic_template_technical, "TYPE_TECHNICAL");
        addTemplate(grid, "极值触碰", "预测金价是否触及目标", R.drawable.ic_template_touch, "TYPE_TOUCH");
        addTemplate(grid, "跑赢率", "黄金 vs BTC 收益率", R.drawable.ic_template_relative, "TYPE_RELATIVE");
    }

    private void addTemplate(GridLayout grid, String title, String desc, int iconRes, String type) {
        View card = LayoutInflater.from(requireContext()).inflate(R.layout.item_gold_template_card, grid, false);
        ((TextView) card.findViewById(R.id.tv_template_title)).setText(title);
        ((TextView) card.findViewById(R.id.tv_template_desc)).setText(desc);
        ((ImageView) card.findViewById(R.id.iv_template_icon)).setImageResource(iconRes);
        
        GridLayout.LayoutParams params = new GridLayout.LayoutParams();
        params.width = 0;
        params.height = ViewGroup.LayoutParams.WRAP_CONTENT;
        params.columnSpec = GridLayout.spec(GridLayout.UNDEFINED, 1f);
        card.setLayoutParams(params);
        
        card.setOnClickListener(v -> {
            Intent intent = new Intent(requireContext(), GoldCreateCustomActivity.class);
            intent.putExtra("TEMPLATE_TYPE", type);
            intent.putExtra("TEMPLATE_TITLE", title);
            startActivity(intent);
        });
        grid.addView(card);
    }
}
