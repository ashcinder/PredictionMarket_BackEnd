package com.example.brokerfi.xc.agent.gold.view;

import android.animation.ValueAnimator;
import android.content.Intent;
import android.graphics.Color;
import android.graphics.Typeface;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.Spannable;
import android.text.SpannableStringBuilder;
import android.text.style.AbsoluteSizeSpan;
import android.text.style.ForegroundColorSpan;
import android.text.style.StyleSpan;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.view.animation.DecelerateInterpolator;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.ViewModelProvider;
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.data.PinataClient;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldPositionValuation;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldMyPositionsViewModel;
import com.bumptech.glide.Glide;

import android.widget.ImageView;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.math.RoundingMode;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public class GoldMyPositionsFragment extends Fragment {
    private static final long DATA_REFRESH_INTERVAL_MS = 15_000L;
    private GoldMyPositionsViewModel viewModel;
    private final List<GoldMarketRepository.GameModel> myPositions = new ArrayList<>();

    private TextView tvTotalBalance, tvTotalPnl;
    private LinearLayout positionsContainer;
    private SwipeRefreshLayout swipeRefresh;
    private double lastTotalBalance = 0.0;
    private final Handler dataRefreshHandler = new Handler(Looper.getMainLooper());
    private final Runnable dataRefreshRunnable = new Runnable() {
        @Override public void run() {
            if (viewModel != null) viewModel.refreshPositions();
            dataRefreshHandler.postDelayed(this, DATA_REFRESH_INTERVAL_MS);
        }
    };

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        View view = inflater.inflate(R.layout.fragment_gold_my_positions, container, false);
        tvTotalBalance = view.findViewById(R.id.tv_total_balance);
        tvTotalPnl = view.findViewById(R.id.tv_total_pnl);
        positionsContainer = view.findViewById(R.id.positions_container);
        swipeRefresh = view.findViewById(R.id.swipe_refresh);
        swipeRefresh.setOnRefreshListener(() -> viewModel.loadPositions());
        return view;
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        viewModel = new ViewModelProvider(this).get(GoldMyPositionsViewModel.class);
        observeViewModel();
        viewModel.loadPositions();
    }

    private void observeViewModel() {
        viewModel.getMyPositions().observe(getViewLifecycleOwner(), positions -> {
            myPositions.clear();
            if (positions != null) {
                myPositions.addAll(positions);
            }
            renderPositions();
            updateSummary();
        });
        viewModel.getIsLoading().observe(getViewLifecycleOwner(), loading -> swipeRefresh.setRefreshing(loading));
        viewModel.getError().observe(getViewLifecycleOwner(), err -> {
            if (err != null) Toast.makeText(requireContext(), "Error: " + err, Toast.LENGTH_SHORT).show();
        });

        viewModel.getDebugToast().observe(getViewLifecycleOwner(), msg -> {
            if (msg != null && !msg.isEmpty()) {
                Toast.makeText(requireContext(), msg, Toast.LENGTH_SHORT).show();
            }
        });
    }

    @Override
    public void onResume() {
        super.onResume();
        dataRefreshHandler.removeCallbacks(dataRefreshRunnable);
        dataRefreshHandler.post(dataRefreshRunnable);
    }

    @Override
    public void onPause() {
        dataRefreshHandler.removeCallbacks(dataRefreshRunnable);
        super.onPause();
    }

    @Override
    public void onDestroyView() {
        dataRefreshHandler.removeCallbacks(dataRefreshRunnable);
        super.onDestroyView();
    }

    private void renderPositions() {
        positionsContainer.removeAllViews();
        LayoutInflater inflater = LayoutInflater.from(requireContext());
        for (GoldMarketRepository.GameModel game : myPositions) {
            View card = inflater.inflate(R.layout.item_gold_position_card, positionsContainer, false);
            TextView tvTitle = card.findViewById(R.id.tv_position_title);
            ImageView ivIcon = card.findViewById(R.id.iv_position_icon);
            String rawTitle = game.desc != null && !game.desc.isEmpty() ? game.desc : "博弈池 #" + game.id;
            tvTitle.setText(styleMarketTitle(rawTitle));

            if (game.avatarUrl != null && !game.avatarUrl.isEmpty()) {
                Glide.with(this).load(PinataClient.IPFS_GATEWAY + game.avatarUrl).placeholder(R.drawable.apartment_icon).into(ivIcon);
            } else {
                ivIcon.setImageResource(R.drawable.apartment_icon);
            }

            TextView tvSide = card.findViewById(R.id.tv_position_side);
            TextView tvShares = card.findViewById(R.id.tv_shares);
            TextView tvCurrentValue = card.findViewById(R.id.tv_current_value);
            TextView tvProfit = card.findViewById(R.id.tv_profit);

            List<String> sideNames = new ArrayList<>();
            StringBuilder shareText = new StringBuilder();
            if (game.myShares != null) {
                for (int i = 0; i < game.myShares.size(); i++) {
                    BigInteger shares = game.myShares.get(i);
                    if (shares == null || shares.compareTo(BigInteger.ZERO) <= 0) continue;
                    String sideName = optionNameFor(game, i);
                    sideNames.add(sideName);
                    if (shareText.length() > 0) shareText.append('\n');
                    shareText.append(sideName).append(": ").append(GoldNoteMarketActivity.formatShareAmount(shares)).append(" 份额");
                }
            }
            tvSide.setText(joinSideNames(sideNames));
            tvSide.setTextColor(resolveSideColor(sideNames));
            tvShares.setText(shareText.length() == 0 ? "暂无份额" : shareText.toString());

            GoldPositionValuation.MarketValue marketValue = GoldPositionValuation.calculateMarket(game);
            tvCurrentValue.setText(marketValue.isComplete() ? GoldNoteMarketActivity.formatBkc(marketValue.getValueWei()) + " BKC" : "暂不可估值");
            tvProfit.setText(game.isRefunded ? "已退款" : (game.isResolved ? "已结算" : "AMM估值"));

            card.setOnClickListener(v -> {
                Intent intent = new Intent(requireContext(), GoldPositionDetailActivity.class);
                intent.putExtra("GAME_ID", game.id);
                intent.putExtra("CONTRACT_ADDRESS", game.contractAddress);
                startActivity(intent);
            });
            positionsContainer.addView(card);
        }
    }

    private void updateSummary() {
        GoldPositionValuation.PortfolioValue portfolio = GoldPositionValuation.calculatePortfolio(myPositions);
        BigDecimal totalBkc = new BigDecimal(portfolio.getValueWei()).divide(new BigDecimal("1000000000000000000"), 6, RoundingMode.HALF_UP);
        animateBalance(totalBkc.doubleValue());
        String subtitle = String.format(Locale.getDefault(), "累计参与 %d 个博弈池", myPositions.size());
        if (portfolio.getUnavailableMarketCount() > 0) {
            subtitle += String.format(Locale.getDefault(), " · %d 个持仓暂未计入估值", portfolio.getUnavailableMarketCount());
        }
        tvTotalPnl.setText(subtitle);
    }

    private String optionNameFor(GoldMarketRepository.GameModel game, int index) {
        if (game.optionNames != null && index < game.optionNames.size()) return game.optionNames.get(index);
        return "选项" + (index + 1);
    }

    private String joinSideNames(List<String> sideNames) {
        if (sideNames.isEmpty()) return "--";
        StringBuilder joined = new StringBuilder();
        for (String s : sideNames) {
            if (joined.length() > 0) joined.append(" / ");
            joined.append(s);
        }
        return joined.toString();
    }

    private int resolveSideColor(List<String> sideNames) {
        if (sideNames.isEmpty()) return Color.BLACK;
        boolean allPositive = true, allNegative = true;
        for (String s : sideNames) {
            String upper = (s == null ? "" : s).toUpperCase(Locale.US);
            boolean pos = upper.contains("YES") || upper.contains("UP") || upper.contains("涨") || upper.contains("达标");
            boolean neg = upper.contains("NO") || upper.contains("DOWN") || upper.contains("跌") || upper.contains("未达标");
            allPositive = allPositive && pos;
            allNegative = allNegative && neg;
        }
        if (allPositive) return 0xFF047857;
        if (allNegative) return Color.RED;
        return Color.BLACK;
    }

    private void animateBalance(double target) {
        ValueAnimator animator = ValueAnimator.ofFloat((float) lastTotalBalance, (float) target);
        animator.setDuration(1000);
        animator.setInterpolator(new DecelerateInterpolator());
        animator.addUpdateListener(animation -> {
            float val = (float) animation.getAnimatedValue();
            tvTotalBalance.setText(String.format(Locale.getDefault(), "%,.2f BKC", (double) val));
        });
        animator.start();
        lastTotalBalance = target;
    }

    private SpannableStringBuilder styleMarketTitle(String title) {
        SpannableStringBuilder ssb = new SpannableStringBuilder(title);
        Pattern datePattern = Pattern.compile("\\d{4}-\\d{2}-\\d{2}");
        Matcher matcher = datePattern.matcher(title);
        while (matcher.find()) {
            ssb.setSpan(new ForegroundColorSpan(0xFF888888), matcher.start(), matcher.end(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            ssb.setSpan(new AbsoluteSizeSpan(11, true), matcher.start(), matcher.end(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
        }
        String[] subjects = {"黄金价格", "黄金波幅", "成交量", "指标", "金价", "黄金收益率"};
        for (String sub : subjects) {
            int start = title.indexOf(sub);
            if (start >= 0) ssb.setSpan(new StyleSpan(Typeface.BOLD), start, start + sub.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
        }
        String[] ups = {"上涨", "剧烈", "高于", "跑赢", "YES", "触及", "达标", "Price Up"};
        for (String kw : ups) {
            int start = title.indexOf(kw);
            if (start >= 0) ssb.setSpan(new ForegroundColorSpan(0xFF047857), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
        }
        String[] downs = {"下跌", "平稳", "低于", "跑输", "NO", "未达标", "Price Down"};
        for (String kw : downs) {
            int start = title.indexOf(kw);
            if (start >= 0) ssb.setSpan(new ForegroundColorSpan(Color.RED), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
        }
        return ssb;
    }
}
