package com.example.brokerfi.xc.agent.gold.view;

import android.app.AlertDialog;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;
import androidx.lifecycle.ViewModelProvider;
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import io.noties.markwon.Markwon;
import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.data.PinataClient;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldGameJudge;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldMarketDetailViewModel;
import com.bumptech.glide.Glide;
import com.github.mikephil.charting.charts.LineChart;
import com.github.mikephil.charting.components.XAxis;
import com.github.mikephil.charting.data.Entry;
import com.github.mikephil.charting.data.LineData;
import com.github.mikephil.charting.data.LineDataSet;
import com.github.mikephil.charting.formatter.ValueFormatter;

import android.widget.ImageView;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

public class GoldMarketDetailActivity extends AppCompatActivity {
    private static final String MARKET_AI_LOADING_MESSAGE = "专属分析仍在生成，请稍后";

    private GoldMarketDetailViewModel viewModel;
    private GoldMarketRepository.GameModel currentGame;
    private int gameId;

    private TextView tvMarketDesc, tvMarketCondition;
    private TextView tvUpPct, tvDownPct, tvPool, tvCountdown, tvHoldings;
    private TextView tvMarketAiStatus, tvMarketAiSummary, tvMarketAiFull;
    private ImageView ivMarketIcon;
    private View barUp, barDown, btnClaimReward, cardMarketAi, layoutAiDetails, layoutChartContainer, viewChartPlaceholder;
    private ProgressBar pbChartLoading;
    private LineChart lineChart;
    private androidx.appcompat.widget.SwitchCompat switchAiManaged;
    private SwipeRefreshLayout swipeRefresh;

    private Markwon markwon;
    private boolean aiExpanded = false;
    private String marketAiSummary = "";
    private String marketAiUnavailableMessage = "";
    private boolean destroyed = false;
    private boolean requestInFlight = false;

    private final Handler timerHandler = new Handler(Looper.getMainLooper());
    private final Runnable countdownRunnable = new Runnable() {
        @Override public void run() {
            if (destroyed) return;
            updateCountdown();
            timerHandler.postDelayed(this, 1000);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_gold_market_detail);
        destroyed = false;
        gameId = getIntent().getIntExtra("GAME_ID", 1);
        viewModel = new ViewModelProvider(this).get(GoldMarketDetailViewModel.class);
        markwon = Markwon.create(this);
        initViews();
        observeViewModel();
        viewModel.loadGameInfo(gameId);
        timerHandler.post(countdownRunnable);
    }

    private void observeViewModel() {
        viewModel.getCurrentGame().observe(this, game -> {
            currentGame = game;
            updateUI();
        });
        viewModel.getMarketHistory().observe(this, this::setupHistoryChart);
        viewModel.getMarketAiSummary().observe(this, this::showMarketAiSummary);
        viewModel.getIsLoading().observe(this, loading -> {
            swipeRefresh.setRefreshing(loading);
            if (loading) {
                // 进入加载状态，重置图表 UI
                pbChartLoading.setVisibility(View.VISIBLE);
                viewChartPlaceholder.setVisibility(View.VISIBLE);
                lineChart.setVisibility(View.INVISIBLE);
            }
        });
        viewModel.getTxStatus().observe(this, status -> {
            if (status != null) Toast.makeText(this, status, Toast.LENGTH_SHORT).show();
        });
        viewModel.getError().observe(this, err -> {
            if (err != null) {
                Toast.makeText(this, err, Toast.LENGTH_SHORT).show();
                showMarketAiUnavailable("Error", err);
            }
        });
    }

    @Override
    protected void onDestroy() {
        destroyed = true;
        timerHandler.removeCallbacks(countdownRunnable);
        super.onDestroy();
    }

    private void initViews() {
        findViewById(R.id.btn_back).setOnClickListener(v -> finish());
        tvMarketDesc = findViewById(R.id.tv_market_desc);
        tvMarketCondition = findViewById(R.id.tv_market_condition);
        tvUpPct = findViewById(R.id.tv_up_pct);
        tvDownPct = findViewById(R.id.tv_down_pct);
        tvPool = findViewById(R.id.tv_pool);
        tvCountdown = findViewById(R.id.tv_countdown);
        tvHoldings = findViewById(R.id.tv_holdings);
        tvMarketAiStatus = findViewById(R.id.tv_market_ai_status);
        tvMarketAiStatus.setText("待启动 ›");
        tvMarketAiSummary = findViewById(R.id.tv_market_ai_summary);
        tvMarketAiSummary.setText("点击卡片启动 AI 专属深度投研分析");
        tvMarketAiFull = findViewById(R.id.tv_market_ai_full);
        ivMarketIcon = findViewById(R.id.iv_market_detail_icon);
        switchAiManaged = findViewById(R.id.switch_ai_managed);
        barUp = findViewById(R.id.bar_up);
        barDown = findViewById(R.id.bar_down);
        cardMarketAi = findViewById(R.id.card_market_ai);
        layoutAiDetails = findViewById(R.id.layout_ai_details);
        layoutChartContainer = findViewById(R.id.layout_chart_container);
        lineChart = findViewById(R.id.line_chart);
        pbChartLoading = findViewById(R.id.pb_chart_loading);
        viewChartPlaceholder = findViewById(R.id.view_chart_placeholder);
        btnClaimReward = findViewById(R.id.btn_claim_reward);

        swipeRefresh = findViewById(R.id.swipe_refresh);
        swipeRefresh.setOnRefreshListener(() -> viewModel.loadGameInfo(gameId));

        findViewById(R.id.btn_buy_up).setOnClickListener(v -> showBuyDialog(0, "YES"));
        findViewById(R.id.btn_buy_down).setOnClickListener(v -> showBuyDialog(1, "NO"));
        cardMarketAi.setOnClickListener(v -> toggleAiDetails());
        switchAiManaged.setOnCheckedChangeListener((btn, isChecked) -> {
            if (currentGame != null && currentGame.isManaged != isChecked) {
                viewModel.toggleAiManaged(gameId, isChecked);
            }
        });
        btnClaimReward.setOnClickListener(v -> claimReward());
    }

    private void updateUI() {
        if (currentGame == null) return;
        tvMarketDesc.setText(currentGame.desc != null && !currentGame.desc.isEmpty() ? currentGame.desc : "博弈池 #" + currentGame.id);
        
        String conditionText = "判定逻辑: " + (currentGame.condition != null ? currentGame.condition : "暂无");
        
        long rem = GoldNoteMarketActivity.remainingSecondsUntilDeadline(currentGame.deadlineSec, System.currentTimeMillis());
        
        if (currentGame.isResolved) {
            String winnerName = (currentGame.optionNames != null && currentGame.winningOption < currentGame.optionNames.size()) 
                    ? currentGame.optionNames.get(currentGame.winningOption) 
                    : (currentGame.winningOption == 0 ? "YES" : "NO");
            conditionText += "\n开奖结果: " + winnerName;
        } else if (currentGame.isRefunded) {
            conditionText += "\n状态: 已退款";
        } else if (rem <= 0) {
            conditionText += "\n状态: 已截止，等待系统开奖...";
        }
        tvMarketCondition.setText(conditionText);
        
        if (currentGame.avatarUrl != null && !currentGame.avatarUrl.isEmpty()) {
            Glide.with(this).load(PinataClient.IPFS_GATEWAY + currentGame.avatarUrl).placeholder(R.drawable.apartment_icon).into(ivMarketIcon);
        } else {
            ivMarketIcon.setImageResource(R.drawable.apartment_icon);
        }

        switchAiManaged.setChecked(currentGame.isManaged);

        btnClaimReward.setVisibility(currentGame.isResolved ? View.VISIBLE : View.GONE);
        
        if (currentGame.virtualReserves != null && currentGame.virtualReserves.size() >= 2) {
            BigInteger res0 = currentGame.virtualReserves.get(0);
            BigInteger res1 = currentGame.virtualReserves.get(1);
            BigInteger total = res0.add(res1);
            if (total.compareTo(BigInteger.ZERO) > 0) {
                float p0 = (float) (res0.doubleValue() / total.doubleValue() * 100);
                float p1 = 100 - p0;
                tvUpPct.setText(String.format(Locale.getDefault(), "%.1f%%", p0));
                tvDownPct.setText(String.format(Locale.getDefault(), "%.1f%%", p1));
                LinearLayout.LayoutParams lp0 = (LinearLayout.LayoutParams) barUp.getLayoutParams();
                lp0.width = 0; lp0.weight = p0; barUp.setLayoutParams(lp0);
                LinearLayout.LayoutParams lp1 = (LinearLayout.LayoutParams) barDown.getLayoutParams();
                lp1.width = 0; lp1.weight = p1; barDown.setLayoutParams(lp1);
            }
        }
        tvPool.setText("总池子: " + GoldNoteMarketActivity.formatBkc(currentGame.totalPool) + " BKC");
        StringBuilder holdings = new StringBuilder();
        if (currentGame.myShares != null) {
            for (int i = 0; i < currentGame.myShares.size(); i++) {
                BigInteger s = currentGame.myShares.get(i);
                if (s == null || s.signum() <= 0) continue;
                if (holdings.length() > 0) holdings.append('\n');
                String name = (currentGame.optionNames != null && i < currentGame.optionNames.size()) ? currentGame.optionNames.get(i) : (i == 0 ? "YES" : "NO");
                holdings.append(name).append(": ").append(GoldNoteMarketActivity.formatShareAmount(s)).append(" 份额");
            }
        }
        tvHoldings.setText(holdings.length() == 0 ? "暂无持仓" : holdings.toString());
        updateCountdown();
    }

    private void setupHistoryChart(final List<GoldMarketRepository.HistoryPoint> history) {
        layoutChartContainer.setVisibility(View.VISIBLE);
        if (history == null || history.isEmpty()) {
            pbChartLoading.setVisibility(View.GONE);
            viewChartPlaceholder.setVisibility(View.VISIBLE);
            lineChart.setVisibility(View.INVISIBLE);
            return;
        }
        
        pbChartLoading.setVisibility(View.GONE);
        viewChartPlaceholder.setVisibility(View.GONE);
        lineChart.setVisibility(View.VISIBLE);

        List<Entry> yesEntries = new ArrayList<>();
        List<Entry> noEntries = new ArrayList<>();
        for (int i = 0; i < history.size(); i++) {
            GoldMarketRepository.HistoryPoint p = history.get(i);
            yesEntries.add(new Entry(i, p.yesPrice));
            noEntries.add(new Entry(i, p.noPrice));
        }

        LineDataSet setYes = new LineDataSet(yesEntries, "YES (看多 %)");
        setYes.setColor(0xFF059669);
        setYes.setCircleColor(0xFF059669);
        setYes.setLineWidth(2.5f);
        setYes.setCircleRadius(3f);
        setYes.setDrawCircleHole(false);
        setYes.setDrawValues(false);
        setYes.setMode(LineDataSet.Mode.CUBIC_BEZIER);
        setYes.setDrawFilled(true);
        setYes.setFillColor(0xFF059669);
        setYes.setFillAlpha(40);

        LineDataSet setNo = new LineDataSet(noEntries, "NO (看空 %)");
        setNo.setColor(0xFFE11D48);
        setNo.setCircleColor(0xFFE11D48);
        setNo.setLineWidth(2.5f);
        setNo.setCircleRadius(3f);
        setNo.setDrawCircleHole(false);
        setNo.setDrawValues(false);
        setNo.setMode(LineDataSet.Mode.CUBIC_BEZIER);
        setNo.setDrawFilled(true);
        setNo.setFillColor(0xFFE11D48);
        setNo.setFillAlpha(40);

        LineData data = new LineData(setYes, setNo);
        lineChart.setData(data);

        lineChart.getDescription().setEnabled(false);
        lineChart.setDrawGridBackground(false);
        lineChart.setTouchEnabled(true);
        lineChart.setScaleEnabled(false);
        lineChart.setPinchZoom(false);
        lineChart.setExtraOffsets(0, 10, 0, 10);
        
        com.github.mikephil.charting.components.Legend legend = lineChart.getLegend();
        legend.setTextColor(0xFF475569);
        legend.setForm(com.github.mikephil.charting.components.Legend.LegendForm.CIRCLE);
        legend.setHorizontalAlignment(com.github.mikephil.charting.components.Legend.LegendHorizontalAlignment.CENTER);

        XAxis xAxis = lineChart.getXAxis();
        xAxis.setPosition(XAxis.XAxisPosition.BOTTOM);
        xAxis.setDrawGridLines(false);
        xAxis.setDrawAxisLine(true);
        xAxis.setAxisLineColor(0xFFE2E8F0);
        xAxis.setTextColor(0xFF94A3B8);
        xAxis.setLabelCount(Math.min(5, history.size()));
        xAxis.setValueFormatter(new ValueFormatter() {
            @Override
            public String getFormattedValue(float value) {
                int idx = (int) value;
                if (history != null && idx >= 0 && idx < history.size()) {
                    long t = history.get(idx).time;
                    java.text.SimpleDateFormat sdf = new java.text.SimpleDateFormat("MM-dd", Locale.getDefault());
                    return sdf.format(new java.util.Date(t * 1000));
                }
                return "";
            }
        });

        lineChart.getAxisRight().setEnabled(false);
        lineChart.getAxisLeft().setDrawGridLines(true);
        lineChart.getAxisLeft().setGridColor(0xFFF1F5F9);
        lineChart.getAxisLeft().setTextColor(0xFF94A3B8);
        lineChart.getAxisLeft().setAxisMaximum(100f);
        lineChart.getAxisLeft().setAxisMinimum(0f);
        lineChart.getAxisLeft().setLabelCount(5);

        lineChart.animateX(1200);
        lineChart.invalidate();
    }

    private void toggleAiDetails() {
        if (marketAiSummary == null || marketAiSummary.isEmpty()) {
            if (!requestInFlight) {
                tvMarketAiStatus.setText("分析中...");
                tvMarketAiSummary.setText("DeepSeek 正在全力解析市场数据，请稍后...");
                requestInFlight = true;
                viewModel.startAiAnalysis();
            }
            return;
        }
        aiExpanded = !aiExpanded;
        layoutAiDetails.setVisibility(aiExpanded ? View.VISIBLE : View.GONE);
        tvMarketAiSummary.setVisibility(aiExpanded ? View.GONE : View.VISIBLE);
        tvMarketAiStatus.setText(aiExpanded ? "收起报告 ˄" : "展开详情 ˅");
    }

    private void showMarketAiSummary(String answer) {
        requestInFlight = false;
        if (destroyed) return;
        if (answer == null || answer.trim().isEmpty()) {
            showMarketAiUnavailable("暂不可用", "AI 分析暂时不可用");
            return;
        }
        marketAiSummary = answer;
        tvMarketAiStatus.setText("展开详情 ˅");
        tvMarketAiSummary.setText(answer);
        markwon.setMarkdown(tvMarketAiFull, answer);
    }

    private void showMarketAiUnavailable(String status, String message) {
        requestInFlight = false;
        if (destroyed) return;
        marketAiUnavailableMessage = message;
        tvMarketAiStatus.setText(status);
        tvMarketAiSummary.setText(message);
    }

    private void updateCountdown() {
        if (currentGame == null) return;
        long rem = GoldNoteMarketActivity.remainingSecondsUntilDeadline(currentGame.deadlineSec, System.currentTimeMillis());
        tvCountdown.setText(GoldNoteMarketActivity.formatRemainingTime(rem));
    }

    private void showBuyDialog(int optionId, String name) {
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        View v = LayoutInflater.from(this).inflate(R.layout.dialog_gold_buy_confirm, null);
        builder.setView(v);
        ((TextView) v.findViewById(R.id.tv_buy_title)).setText("确认下注 " + name);
        EditText et = v.findViewById(R.id.et_buy_amount);
        Button btn = v.findViewById(R.id.btn_buy_confirm);
        btn.setBackgroundResource(optionId == 0 ? R.drawable.bg_bet_yes : R.drawable.bg_bet_no);
        AlertDialog dialog = builder.create();
        if (dialog.getWindow() != null) dialog.getWindow().setBackgroundDrawableResource(android.R.color.transparent);
        btn.setOnClickListener(view -> {
            String val = et.getText().toString().trim();
            if (val.isEmpty()) return;
            BigInteger wei = GoldMarketRepository.parseTokenAmountToWei(val);
            if (wei == null) return;
            dialog.dismiss();
            viewModel.buyShares(gameId, optionId, wei);
        });
        v.findViewById(R.id.btn_buy_cancel).setOnClickListener(view -> dialog.dismiss());
        dialog.show();
    }

    private void claimReward() {
        if (currentGame == null) return;
        if (!currentGame.isResolved) {
            Toast.makeText(this, "博弈尚未开奖，无法领取", Toast.LENGTH_SHORT).show();
            return;
        }
        int optIndex = currentGame.winningOption;
        if (optIndex < 0 || optIndex >= currentGame.myShares.size() || currentGame.myShares.get(optIndex).signum() <= 0) {
            Toast.makeText(this, "该选项您没有中奖份额可领取", Toast.LENGTH_SHORT).show();
            return;
        }
        viewModel.claimReward(gameId, optIndex);
    }
}
