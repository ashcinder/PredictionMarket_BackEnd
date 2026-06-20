package com.example.brokerfi.xc.agent.gold.view;

import android.app.AlertDialog;
import android.content.Intent;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;
import androidx.lifecycle.ViewModelProvider;
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import io.noties.markwon.Markwon;
import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldGameJudge;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldMarketDetailViewModel;

import java.math.BigInteger;
import java.util.Locale;

public class GoldMarketDetailActivity extends AppCompatActivity {
    private static final String MARKET_AI_LOADING_MESSAGE = "专属分析仍在生成，请稍后";

    private GoldMarketDetailViewModel viewModel;
    private GoldMarketRepository.GameModel currentGame;
    private int gameId;

    private TextView tvMarketDesc, tvMarketCondition;
    private TextView tvUpPct, tvDownPct, tvPool, tvCountdown, tvHoldings;
    private TextView tvMarketAiStatus, tvMarketAiSummary, tvMarketAiFull;
    private View barUp, barDown, btnClaimReward, btnAdminResolve, cardMarketAi, layoutAiDetails;
    private androidx.appcompat.widget.SwitchCompat switchAiManaged;
    private SwipeRefreshLayout swipeRefresh;

    private Markwon markwon;
    private boolean aiExpanded = false;
    private String marketAiSummary = "";
    private String marketAiUnavailableMessage = "";
    private boolean destroyed = false;
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
        viewModel.getMarketAiSummary().observe(this, summary -> {
            marketAiSummary = summary;
            showMarketAiSummary(summary);
        });
        viewModel.getIsLoading().observe(this, loading -> swipeRefresh.setRefreshing(loading));
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
        tvMarketAiSummary = findViewById(R.id.tv_market_ai_summary);
        tvMarketAiFull = findViewById(R.id.tv_market_ai_full);
        switchAiManaged = findViewById(R.id.switch_ai_managed);
        barUp = findViewById(R.id.bar_up);
        barDown = findViewById(R.id.bar_down);
        cardMarketAi = findViewById(R.id.card_market_ai);
        layoutAiDetails = findViewById(R.id.layout_ai_details);
        btnClaimReward = findViewById(R.id.btn_claim_reward);
        btnAdminResolve = findViewById(R.id.btn_admin_resolve);

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
        btnAdminResolve.setOnClickListener(v -> performAdminResolve());
    }

    private void updateUI() {
        if (currentGame == null) return;
        tvMarketDesc.setText(currentGame.desc != null && !currentGame.desc.isEmpty() ? currentGame.desc : "博弈池 #" + currentGame.id);
        tvMarketCondition.setText("判定逻辑: " + (currentGame.condition != null ? currentGame.condition : "暂无"));
        
        // 更新托管状态开关，不触发 Listener
        switchAiManaged.setChecked(currentGame.isManaged);

        long rem = GoldNoteMarketActivity.remainingSecondsUntilDeadline(currentGame.deadlineSec, System.currentTimeMillis());
        if (rem <= 0 && !currentGame.isResolved && !currentGame.isRefunded) {
            GoldAdvisoryManager.fetchPrice(new GoldAdvisoryManager.AdvisoryCallback() {
                @Override public void onSuccess(GoldAdvisoryManager.Advisory quote) {
                    int winner = GoldGameJudge.evaluateGameWinner(currentGame, quote);
                    String name = (currentGame.optionNames != null && winner < currentGame.optionNames.size()) ? currentGame.optionNames.get(winner) : (winner == 0 ? "YES" : "NO");
                    tvMarketCondition.setText("判定逻辑: " + currentGame.condition + "\n系统判定胜出: " + name);
                }
                @Override public void onError(String error) {}
            });
        }
        btnClaimReward.setVisibility(currentGame.isResolved || currentGame.isRefunded ? View.VISIBLE : View.GONE);
        btnAdminResolve.setVisibility(rem <= 0 && !currentGame.isResolved && !currentGame.isRefunded ? View.VISIBLE : View.GONE);
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

    private void showMarketAiSummary(String answer) {
        if (destroyed) return;
        if (answer == null || answer.trim().isEmpty()) {
            showMarketAiUnavailable("暂不可用", "AI 分析暂时不可用");
            return;
        }
        tvMarketAiStatus.setText("展开详情 ˅");
        tvMarketAiSummary.setText(answer);
        markwon.setMarkdown(tvMarketAiFull, answer);
    }

    private void toggleAiDetails() {
        if (marketAiSummary == null || marketAiSummary.isEmpty()) {
            String msg = marketAiUnavailableMessage.isEmpty() ? MARKET_AI_LOADING_MESSAGE : marketAiUnavailableMessage;
            Toast.makeText(this, msg, Toast.LENGTH_SHORT).show();
            return;
        }
        aiExpanded = !aiExpanded;
        layoutAiDetails.setVisibility(aiExpanded ? View.VISIBLE : View.GONE);
        tvMarketAiSummary.setVisibility(aiExpanded ? View.GONE : View.VISIBLE);
        tvMarketAiStatus.setText(aiExpanded ? "收起报告 ˄" : "展开详情 ˅");
    }

    private void showMarketAiUnavailable(String status, String message) {
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

    private void performAdminResolve() {
        if (currentGame == null) return;
        Toast.makeText(this, "获取行情中...", Toast.LENGTH_SHORT).show();
        GoldAdvisoryManager.fetchPrice(new GoldAdvisoryManager.AdvisoryCallback() {
            @Override public void onSuccess(GoldAdvisoryManager.Advisory quote) {
                int winner = GoldGameJudge.evaluateGameWinner(currentGame, quote);
                String name = (currentGame.optionNames != null && winner < currentGame.optionNames.size()) ? currentGame.optionNames.get(winner) : (winner == 0 ? "YES" : "NO");
                new AlertDialog.Builder(GoldMarketDetailActivity.this).setTitle("管理员开奖确认").setMessage("判定结果: " + name + "\n确定要执行链上结算吗？").setPositiveButton("立即结算", (d, w) -> {
                    // Admin resolve in Repo for now
                }).setNegativeButton("取消", null).show();
            }
            @Override public void onError(String e) { Toast.makeText(GoldMarketDetailActivity.this, "获取失败", Toast.LENGTH_SHORT).show(); }
        });
    }

    private void claimReward() {
        if (currentGame == null) return;
        int optIndex = -1;
        if (currentGame.isResolved) optIndex = currentGame.winningOption;
        else if (currentGame.isRefunded) {
            for (int i=0; i<currentGame.myShares.size(); i++) {
                if (currentGame.myShares.get(i).compareTo(BigInteger.ZERO) > 0) { optIndex = i; break; }
            }
        }
        if (optIndex != -1) viewModel.claimReward(gameId, optIndex);
    }
}
