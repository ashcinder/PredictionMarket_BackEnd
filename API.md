# BrokerWallet Android应用 - DAO功能结构说明

## 📁 DAO功能概览

本文档专门描述Android应用中与DAO（去中心化自治组织）相关的所有代码和文件。DAO功能包括：勋章排行榜、证明材料提交、NFT展示、个人中心和全局统计。

**注意：** DAO功能界面已全面英文化，提供更好的国际化体验。

---

## 🗂️ DAO相关文件结构

```
brokerwallet-academic/
└── app/src/main/
    ├── java/com/example/brokerfi/xc/
    │   ├── MedalRankingActivity.java        # 勋章排行榜（核心）
    │   ├── ProofAndNFTActivity.java         # 证明提交与NFT铸造
    │   ├── MyCenterActivity.java            # 个人中心（我的勋章、提交历史、我的NFT）
    │   ├── GlobalStatsActivity.java         # 全局统计（全局勋章、全局NFT）
    │   │
    │   ├── adapter/                         # 列表适配器
    │   │   ├── MedalRankingAdapter.java     # 勋章排行榜适配器
    │   │   ├── NFTViewAdapter.java          # NFT列表适配器
    │   │   └── SubmissionHistoryAdapter.java # 提交历史适配器
    │   │
    │   ├── model/                           # 数据模型
    │   │   ├── SubmissionRecord.java        # 提交记录模型
    │   │   └── NFT.java                     # NFT数据模型
    │   │
    │   ├── dto/                             # 数据传输对象
    │   │   └── MedalQueryResult.java        # 勋章查询结果
    │   │
    │   └── util/                            # 工具类
    │       ├── ProofUploadUtil.java         # 证明上传工具
    │       ├── SubmissionUtil.java          # 提交工具
    │       ├── MedalApiUtil.java            # 勋章API工具
    │       └── NFTApiUtil.java              # NFT API工具
    │
    └── res/
        ├── layout/                          # 布局文件
        │   ├── activity_medal_ranking.xml   # 勋章排行榜布局
        │   ├── activity_proof_and_nft.xml   # 证明提交布局
        │   ├── activity_my_center.xml       # 个人中心布局
        │   ├── activity_global_stats.xml    # 全局统计布局
        │   ├── item_submission_history.xml  # 提交历史项布局
        │   └── dialog_nft_detail.xml        # NFT详情对话框布局
        │
        ├── drawable/                        # 图标资源
        │   ├── dao_team.xml                 # DAO团队图标
        │   └── dao_team_icon.xml            # DAO图标
        │
        └── values/
            └── strings.xml                  # 字符串资源（已英文化）
```

---

## 🔑 核心Activity说明

### 1. MedalRankingActivity.java - 勋章排行榜

**位置：** `app/src/main/java/com/example/brokerfi/xc/MedalRankingActivity.java`

**功能：**
- ✅ 显示全局勋章排行榜
- ✅ 显示我的排名和勋章
- ✅ 支持下拉刷新
- ✅ 点击用户卡片查看详情
- ✅ 显示用户昵称和代表作
- ✅ 计算说明弹窗（英文）

**关键功能：**

```java
// 查询全局排行榜
private void loadGlobalRanking() {
    String url = BaseUrl.getBaseUrl() + "/api/medals/ranking";
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseRankingData(result);
        }
    });
}

// 查询我的勋章
private void loadMyMedals() {
    String address = getMyAddress();
    String url = BaseUrl.getBaseUrl() + "/api/medals/user/" + address;
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseMyMedals(result);
        }
    });
}

// 显示计算说明对话框（英文）
private void showCalculationHelpDialog() {
    AlertDialog.Builder builder = new AlertDialog.Builder(this);
    builder.setTitle("🏆 Medal Ranking Calculation");
    builder.setMessage(
        "📊 Score Calculation Formula:\n" +
        "Total Score = Gold × 3 + Silver × 2 + Bronze × 1\n\n" +
        "🥇 Gold Medal = 3 points\n" +
        "🥈 Silver Medal = 2 points\n" +
        "🥉 Bronze Medal = 1 point\n\n" +
        "📈 Ranking Rules:\n" +
        "1. Sorted by total score (descending)\n" +
        "2. If tied, sorted by gold medals\n" +
        "3. If tied, sorted by silver medals\n" +
        "4. If tied, sorted by bronze medals"
    );
    builder.setPositiveButton("Submit Now", ...);
    builder.show();
}
```

**布局文件：** `res/layout/activity_medal_ranking.xml`

**关键UI元素：**
- 顶部标题："🏆 Medal Ranking"
- 两个标签页："📊 Global" 和 "👤 My"
- 提交证明按钮："📄 Proof Submit"
- 计算说明按钮："❓"
- RecyclerView显示排行榜列表

---

### 2. ProofAndNFTActivity.java - 证明提交与NFT铸造

**位置：** `app/src/main/java/com/example/brokerfi/xc/ProofAndNFTActivity.java`

**功能：**
- ✅ 批量上传证明文件（支持多文件）
- ✅ 上传NFT图片（可选）
- ✅ 设置显示昵称
- ✅ 选择是否显示代表作
- ✅ 文件选择帮助说明（英文）
- ✅ 提交成功提示（英文）

**关键功能：**

```java
// 选择证明文件（支持多选）
private void selectProofFiles() {
    Intent intent = new Intent(Intent.ACTION_GET_CONTENT);
    intent.setType("*/*");
    intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);  // 允许多选
    startActivityForResult(intent, REQUEST_CODE_PROOF_FILES);
}

// 选择NFT图片
private void selectNftImage() {
    Intent intent = new Intent(Intent.ACTION_PICK);
    intent.setType("image/*");
    startActivityForResult(intent, REQUEST_CODE_NFT_IMAGE);
}

// 提交所有文件
private void submitAll() {
    String walletAddress = getMyAddress();
    String displayName = displayNameInput.getText().toString();
    boolean showWork = showWorkRadioGroup.getCheckedRadioButtonId() == R.id.showWorkYes;
    
    // 使用工具类上传
    ProofUploadUtil.uploadBatch(
        this,
        walletAddress,
        selectedProofFiles,
        selectedNftImageUri,
        displayName,
        showWork,
        new ProofUploadUtil.UploadCallback() {
            @Override
            public void onSuccess(String message) {
                showSuccessMessage(message);
            }
            
            @Override
            public void onError(String error) {
                Toast.makeText(ProofAndNFTActivity.this, error, Toast.LENGTH_LONG).show();
            }
        }
    );
}

// 显示成功消息（英文）
private void showSuccessMessage(String message) {
    AlertDialog.Builder builder = new AlertDialog.Builder(this);
    builder.setTitle("✅ Submission Success");
    builder.setMessage(
        "Submission completed!\n\n" +
        "📄 Proof files uploaded, waiting for admin review\n" +
        "🖼️ NFT image uploaded (if provided)\n" +
        "⏳ Please wait for review results\n\n" +
        "You can check submission status in 'My Center'"
    );
    builder.setPositiveButton("OK", ...);
    builder.show();
}

// 文件选择帮助（英文）
private void showFileHelpDialog() {
    AlertDialog.Builder builder = new AlertDialog.Builder(this);
    builder.setTitle("💡 File Selection Tips");
    builder.setMessage(
        "To select files from apps like WeChat, follow these steps:\n\n" +
        "1️⃣ Find the file you want to upload in WeChat\n" +
        "2️⃣ Long press the file, select 「Forward」\n" +
        "3️⃣ Choose 「Save to Files」 or 「More」\n" +
        "4️⃣ Save the file to phone storage\n" +
        "5️⃣ Return to this page, click 「Select Proof File」 to find the saved file"
    );
    builder.show();
}
```

**布局文件：** `res/layout/activity_proof_and_nft.xml`

**关键UI元素：**
- 标题："📄 Proof Submission"
- 证明文件选择："Submit Proof File *"
- NFT图片选择："Select Photo for NFT Minting (Optional)"
- 昵称输入框："Enter your preferred display nickname"
- 代表作显示选项："Yes" / "No"
- 提交按钮："Submit Proof"

---

### 3. MyCenterActivity.java - 个人中心

**位置：** `app/src/main/java/com/example/brokerfi/xc/MyCenterActivity.java`

**功能：**
- ✅ 显示我的勋章统计
- ✅ 显示提交历史（分页加载）
- ✅ 显示我的NFT（分页加载）
- ✅ 地址切换时自动清空缓存
- ✅ 支持下拉刷新
- ✅ NFT详情查看（无关闭按钮）

**关键功能：**

```java
// 静态缓存变量
private static List<SubmissionRecord> cachedSubmissionList = new ArrayList<>();
private static List<NFT> cachedNftList = new ArrayList<>();
private static String cachedWalletAddress = null;  // 缓存的钱包地址

// 检查并恢复NFT缓存（地址切换检测）
private void checkAndRestoreNftCache() {
    String currentAddress = getMyAddressForDatabase();
    
    if (cachedWalletAddress != null && cachedWalletAddress.equals(currentAddress)) {
        // 地址未变，恢复缓存
        if (cachedNftList != null && !cachedNftList.isEmpty()) {
            nftList.clear();
            nftList.addAll(cachedNftList);
            nftHasMore = cachedNftHasMore;
            totalNftCount = cachedTotalNftCount;
            Log.d("MyCenter", "Address unchanged, restored NFT cache: " + nftList.size());
        }
    } else {
        // 地址改变，清空旧缓存
        if (cachedWalletAddress != null) {
            Log.d("MyCenter", "Address changed from " + cachedWalletAddress + 
                  " to " + currentAddress + ", clearing cache");
        }
        clearNftCache();
    }
}

// 清空NFT缓存
private void clearNftCache() {
    cachedNftList = new ArrayList<>();
    cachedNftHasMore = true;
    cachedTotalNftCount = 0;
    cachedWalletAddress = null;
    Log.d("MyCenter", "NFT cache cleared");
}

// 保存NFT缓存
private void saveNftCache() {
    cachedNftList = new ArrayList<>(nftList);
    cachedNftHasMore = nftHasMore;
    cachedTotalNftCount = totalNftCount;
    cachedWalletAddress = getMyAddressForDatabase();  // 保存当前地址
    Log.d("MyCenter", "NFT cache saved: " + cachedNftList.size() + " items, address=" + cachedWalletAddress);
}

// 加载我的勋章
private void loadMyMedals() {
    String address = getMyAddressForDatabase();
    String url = BaseUrl.getBaseUrl() + "/api/medals/user/" + address;
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseMyMedals(result);
        }
    });
}

// 加载提交历史（分页）
private void loadSubmissionHistory(int page) {
    String address = getMyAddressForDatabase();
    String url = BaseUrl.getBaseUrl() + "/api/files/submission-history?walletAddress=" + 
                 address + "&page=" + page + "&size=" + PAGE_SIZE;
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseSubmissionHistory(result, page);
        }
    });
}

// 加载我的NFT（分页）
private void loadMyNfts(int page) {
    String address = getMyAddressForDatabase();
    String url = BaseUrl.getBaseUrl() + "/api/blockchain/nfts/user/" + 
                 address + "?page=" + page + "&size=" + PAGE_SIZE;
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseMyNfts(result, page);
        }
    });
}

// 显示NFT详情（无关闭按钮）
private void showNftDetail(NFT nft) {
    Dialog dialog = new Dialog(this);
    dialog.setContentView(R.layout.dialog_nft_detail);
    
    // 设置NFT信息
    ImageView nftImage = dialog.findViewById(R.id.nftImageView);
    TextView uploadTimeText = dialog.findViewById(R.id.uploadTimeText);
    TextView mintTimeText = dialog.findViewById(R.id.mintTimeText);
    // ... 其他UI元素
    
    // 加载图片
    Glide.with(this).load(nft.getImageUrl()).into(nftImage);
    
    // 点击外部或返回键关闭
    dialog.setCanceledOnTouchOutside(true);
    dialog.show();
}
```

**布局文件：** `res/layout/activity_my_center.xml`

**关键UI元素：**
- 标题："👤 My"
- 勋章统计卡片："🏆 My Medals"
  - 金牌数："Gold: X"
  - 银牌数："Silver: X"
  - 铜牌数："Bronze: X"
  - 总分："Total: X"
- 提交历史标签："📝 Submission History"
- NFT标签："🖼️ My NFTs"
- 空状态提示："No Submission History" / "No NFTs yet"

---

### 4. GlobalStatsActivity.java - 全局统计

**位置：** `app/src/main/java/com/example/brokerfi/xc/GlobalStatsActivity.java`

**功能：**
- ✅ 显示全局勋章统计
- ✅ 显示全局NFT画廊（分页加载）
- ✅ 支持下拉刷新
- ✅ NFT详情查看

**关键功能：**

```java
// 加载全局统计
private void loadGlobalStats() {
    String url = BaseUrl.getBaseUrl() + "/api/medals/global-stats";
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseGlobalStatsData(result);
        }
    });
}

// 解析全局统计数据（英文显示）
private void parseGlobalStatsData(String result) {
    JSONObject data = new JSONObject(result);
    
    totalUsersText.setText("Total Users: " + data.optInt("totalUsers", 0));
    highestScoreText.setText("Highest Score: " + data.optInt("highestScore", 0));
    
    String topUser = data.optString("topUserDisplayName", "None");
    if (topUser.equals("null") || topUser.isEmpty()) {
        topUser = "None";
    }
    topUserText.setText("Top User: " + topUser);
    
    goldCountText.setText(String.valueOf(data.optInt("totalGold", 0)));
    silverCountText.setText(String.valueOf(data.optInt("totalSilver", 0)));
    bronzeCountText.setText(String.valueOf(data.optInt("totalBronze", 0)));
}

// 加载全局NFT（分页）
private void loadGlobalNfts(int page) {
    String url = BaseUrl.getBaseUrl() + "/api/blockchain/nfts/all?page=" + 
                 page + "&size=" + PAGE_SIZE;
    OkhttpUtils.get(url, new MyCallBack() {
        @Override
        public void onSuccess(String result) {
            parseGlobalNfts(result, page);
        }
    });
}
```

**布局文件：** `res/layout/activity_global_stats.xml`

**关键UI元素：**
- 标题："📊 Global Stats"
- 全局勋章统计卡片："🏆 Global Medal Stats"
  - 总用户数："Total Users: X"
  - 最高分："Highest Score: X"
  - 榜首用户："Top User: XXX"
  - 金银铜牌总数
- NFT画廊："🎨 Global NFT Gallery"
- 空状态："No NFTs minted globally yet!"

---

## 📦 适配器说明

### 1. MedalRankingAdapter.java - 勋章排行榜适配器

**位置：** `app/src/main/java/com/example/brokerfi/xc/adapter/MedalRankingAdapter.java`

**功能：**
- ✅ 显示用户排名卡片
- ✅ 显示用户昵称（或"Anonymous"）
- ✅ 显示勋章数量和总分
- ✅ 显示代表作品（如果有）

**关键代码：**
```java
@Override
public void onBindViewHolder(@NonNull ViewHolder holder, int position) {
    MedalRankingItem item = rankingList.get(position);
    
    // 排名
    holder.rankText.setText(String.valueOf(item.getRank()));
    
    // 昵称（英文）
    holder.displayNameText.setText(
        item.getDisplayName() != null && !item.getDisplayName().isEmpty() 
        ? item.getDisplayName() 
        : "Anonymous"
    );
    
    // 勋章数量
    holder.goldText.setText(String.valueOf(item.getGoldMedals()));
    holder.silverText.setText(String.valueOf(item.getSilverMedals()));
    holder.bronzeText.setText(String.valueOf(item.getBronzeMedals()));
    
    // 总分（英文）
    holder.totalMedalText.setText("Total: " + item.getTotalMedalScore());
    
    // 代表作（英文）
    if (item.isShowRepresentativeWork() && item.getRepresentativeWork() != null) {
        holder.representativeWorkText.setVisibility(View.VISIBLE);
        holder.representativeWorkText.setText("Work: " + item.getRepresentativeWork());
    } else {
        holder.representativeWorkText.setVisibility(View.GONE);
    }
}
```

---

### 2. NFTViewAdapter.java - NFT列表适配器

**位置：** `app/src/main/java/com/example/brokerfi/xc/adapter/NFTViewAdapter.java`

**功能：**
- ✅ 显示NFT图片（使用Glide加载）
- ✅ 显示时间信息（英文）
- ✅ 显示持有者信息
- ✅ 支持分页加载（Footer显示加载状态）

**关键代码：**
```java
@Override
public void onBindViewHolder(@NonNull RecyclerView.ViewHolder holder, int position) {
    if (holder instanceof ViewHolder) {
        NFT nft = nftList.get(position);
        ViewHolder vh = (ViewHolder) holder;
        
        // 加载NFT图片
        Glide.with(context)
            .load(nft.getImageUrl())
            .placeholder(R.drawable.ic_image_placeholder)
            .error(R.drawable.ic_image_error)
            .into(vh.nftImageView);
        
        // 时间信息（英文）
        String uploadTime = nft.getUploadTime() != null ? nft.getUploadTime() : "Unknown";
        String mintTime = nft.getMintTime() != null ? nft.getMintTime() : "Unknown";
        
        vh.uploadTimeText.setText("Material Upload: " + uploadTime);
        vh.mintTimeText.setText("NFT Minted: " + mintTime);
        
        // 持有者信息（英文）
        String ownerAddress = nft.getOwnerAddress();
        String shortAddress = ownerAddress.substring(0, 6) + "..." + 
                             ownerAddress.substring(ownerAddress.length() - 4);
        vh.ownerAddressText.setText("Owner Address: " + shortAddress);
        
        String ownerDisplayName = nft.getOwnerDisplayName() != null && 
                                 !nft.getOwnerDisplayName().isEmpty()
                                 ? nft.getOwnerDisplayName() 
                                 : "Anonymous";
        vh.ownerDisplayNameText.setText("Owner Nickname: " + ownerDisplayName);
        
        // 点击查看详情
        vh.itemView.setOnClickListener(v -> {
            if (onItemClickListener != null) {
                onItemClickListener.onItemClick(nft);
            }
        });
    } else if (holder instanceof FooterViewHolder) {
        FooterViewHolder fh = (FooterViewHolder) holder;
        
        // Footer状态（英文）
        if (isLoading) {
            fh.footerText.setText("Loading...");
            fh.progressBar.setVisibility(View.VISIBLE);
        } else if (hasMore) {
            fh.footerText.setText("Pull up to load more");
            fh.progressBar.setVisibility(View.GONE);
        } else {
            fh.footerText.setText("End of list ~ Submit materials to get more NFTs");
            fh.progressBar.setVisibility(View.GONE);
        }
    }
}
```

---

### 3. SubmissionHistoryAdapter.java - 提交历史适配器

**位置：** `app/src/main/java/com/example/brokerfi/xc/adapter/SubmissionHistoryAdapter.java`

**功能：**
- ✅ 显示提交记录卡片
- ✅ 显示文件列表
- ✅ 显示审核状态（英文）
- ✅ 显示勋章信息（英文）
- ✅ 显示进度条（英文）

**关键代码：**
```java
@Override
public void onBindViewHolder(@NonNull ViewHolder holder, int position) {
    SubmissionRecord record = recordList.get(position);
    
    // 文件列表（英文）
    List<String> fileNames = record.getFileNames();
    if (fileNames != null && !fileNames.isEmpty()) {
        String firstFile = fileNames.get(0);
        if (fileNames.size() > 1) {
            holder.fileNameText.setText(firstFile + " and " + 
                                       (fileNames.size() - 1) + " more file(s)");
        } else {
            holder.fileNameText.setText(firstFile);
        }
    }
    
    // 审核状态（英文）
    String status = record.getAuditStatus();
    if ("APPROVED".equals(status)) {
        holder.statusText.setText("Approved");
        holder.statusText.setTextColor(Color.parseColor("#4CAF50"));
    } else if ("REJECTED".equals(status)) {
        holder.statusText.setText("Rejected");
        holder.statusText.setTextColor(Color.parseColor("#F44336"));
    } else {
        holder.statusText.setText("Pending");
        holder.statusText.setTextColor(Color.parseColor("#FF9800"));
    }
    
    // 勋章信息（英文）
    String medalInfo = buildMedalInfo(record.getMedalAwarded());
    holder.medalText.setText(medalInfo);
    
    // 进度条（英文）
    updateProgress(holder, record);
}

// 构建勋章信息（英文）
private String buildMedalInfo(String medalAwarded) {
    if (medalAwarded == null || "NONE".equals(medalAwarded)) {
        return "⚪ No Medal Awarded";
    } else {
        return "🏅 Medal Awarded";
    }
}

// 更新进度（英文）
private void updateProgress(ViewHolder holder, SubmissionRecord record) {
    String progressStr = "1/3 Uploaded";
    int progress = 33;
    
    if ("APPROVED".equals(record.getAuditStatus())) {
        progressStr = "2/3 Approved";
        progress = 66;
        
        if (record.getMedalAwarded() != null && !"NONE".equals(record.getMedalAwarded())) {
            if (record.isHasNftImage()) {
                progressStr = "3/3 NFT Minted";
            } else {
                progressStr = "3/3 Medal Awarded";
            }
            progress = 100;
        }
    } else if ("REJECTED".equals(record.getAuditStatus())) {
        progressStr = "Audit Rejected";
        progress = 0;
    }
    
    holder.progressText.setText(progressStr);
    holder.progressBar.setProgress(progress);
}
```

---

## 🎨 布局文件说明

### 1. dialog_nft_detail.xml - NFT详情对话框

**位置：** `res/layout/dialog_nft_detail.xml`

**重要变更：**
- ✅ 移除了底部的"Close"按钮
- ✅ 用户可以点击外部区域或返回键关闭对话框
- ✅ 所有文本已英文化

**关键元素：**
```xml
<LinearLayout>
    <!-- NFT图片 -->
    <TextView android:text="NFT Image" />
    <ImageView android:id="@+id/nftImageView" />
    
    <!-- 时间信息 -->
    <TextView android:text="Time Information" />
    <TextView android:id="@+id/uploadTimeText" 
              android:text="Material Upload: 2025-10-10" />
    <TextView android:id="@+id/mintTimeText" 
              android:text="NFT Minted: 2025-10-10" />
    
    <!-- 持有者信息 -->
    <TextView android:id="@+id/ownerAddressText" 
              android:text="Owner Address: 0x..." />
    <TextView android:id="@+id/ownerDisplayNameText" 
              android:text="Owner Nickname: Anonymous" />
    
    <!-- 已移除Close按钮 -->
</LinearLayout>
```

---

### 2. item_submission_history.xml - 提交历史项布局

**位置：** `res/layout/item_submission_history.xml`

**关键元素（已英文化）：**
```xml
<androidx.cardview.widget.CardView>
    <LinearLayout>
        <!-- 文件名 -->
        <TextView android:id="@+id/fileNameText" 
                  android:text="file.pdf and 2 more file(s)" />
        
        <!-- 审核状态 -->
        <TextView android:id="@+id/statusText" 
                  android:text="Pending" />
        
        <!-- 勋章信息 -->
        <TextView android:id="@+id/medalText" 
                  android:text="⚪ No Medal" />
        
        <!-- NFT状态 -->
        <TextView android:id="@+id/nftStatusText" 
                  android:text="🖼️ Not Started" />
        
        <!-- 代币奖励 -->
        <TextView android:id="@+id/tokenRewardText" 
                  android:text="💰 BKC Reward: 10.5 BKC" />
        
        <!-- 进度条 -->
        <ProgressBar android:id="@+id/progressBar" />
        <TextView android:id="@+id/progressText" 
                  android:text="1/3 Uploaded" />
    </LinearLayout>
</androidx.cardview.widget.CardView>
```

---

## 🔧 工具类说明

### 1. ProofUploadUtil.java - 证明上传工具

**位置：** `app/src/main/java/com/example/brokerfi/xc/ProofUploadUtil.java`

**功能：**
- ✅ 批量上传证明文件
- ✅ 上传NFT图片
- ✅ 生成批次ID
- ✅ 错误处理（英文）

**关键方法：**
```java
// 批量上传
public static void uploadBatch(
    Context context,
    String walletAddress,
    List<Uri> proofFiles,
    Uri nftImageUri,
    String displayName,
    boolean showWork,
    UploadCallback callback
) {
    // 1. 生成批次ID
    String batchId = generateBatchId();
    
    // 2. 上传证明文件
    uploadProofFiles(context, walletAddress, proofFiles, batchId, ...);
    
    // 3. 上传NFT图片（如果有）
    if (nftImageUri != null) {
        uploadNftImage(context, walletAddress, nftImageUri, batchId, ...);
    }
    
    // 4. 回调成功
    callback.onSuccess("Submission completed!");
}

// 错误消息解析（英文）
private static String parseErrorMessage(String errorBody, int statusCode) {
    try {
        JSONObject jsonError = new JSONObject(errorBody);
        
        // 检查是否是重复NFT图片错误
        if (jsonError.has("errorCode") && 
            "DUPLICATE_NFT_IMAGE".equals(jsonError.optString("errorCode"))) {
            return "NFT image uniqueness constraint: This NFT already exists, " +
                   "please select a different image to mint";
        }
        
        // 其他错误
        if (jsonError.has("message")) {
            return jsonError.getString("message");
        }
    } catch (Exception e) {
        // 解析失败，返回默认错误消息
    }
    
    // 根据状态码返回默认消息（英文）
    if (statusCode == 400) {
        return "Upload failed, please check file format and content";
    } else if (statusCode == 500) {
        return "Server error, please try again later";
    } else {
        return "Upload failed (Error code: " + statusCode + ")";
    }
}
```

---

## 🌐 字符串资源（英文化）

### strings.xml

**位置：** `res/values/strings.xml`

**DAO相关字符串（已英文化）：**
```xml
<resources>
    <!-- 勋章排行榜 -->
    <string name="medal_ranking">Medal Ranking</string>
    <string name="global_ranking">Global</string>
    <string name="my_ranking">My</string>
    <string name="gold_medal">Gold</string>
    <string name="silver_medal">Silver</string>
    <string name="bronze_medal">Bronze</string>
    <string name="total_score">Total</string>
    
    <!-- 证明提交 -->
    <string name="proof_submission">Proof Submission</string>
    <string name="select_proof_file">Submit Proof File</string>
    <string name="select_nft_image">Select Photo for NFT Minting (Optional)</string>
    <string name="display_name_hint">Enter your preferred display nickname</string>
    <string name="show_representative_work">Display representative work on ranking</string>
    <string name="submit_proof">Submit Proof</string>
    
    <!-- 个人中心 -->
    <string name="my_center">My</string>
    <string name="my_medals">My Medals</string>
    <string name="submission_history">Submission History</string>
    <string name="my_nfts">My NFTs</string>
    <string name="no_submission_history">No Submission History</string>
    <string name="no_nfts">No NFTs yet</string>
    
    <!-- 全局统计 -->
    <string name="global_stats">Global Stats</string>
    <string name="global_medal_stats">Global Medal Stats</string>
    <string name="total_users">Total Users</string>
    <string name="highest_score">Highest Score</string>
    <string name="top_user">Top User</string>
    <string name="global_nft_gallery">Global NFT Gallery</string>
    
    <!-- 状态 -->
    <string name="pending">Pending</string>
    <string name="approved">Approved</string>
    <string name="rejected">Rejected</string>
    <string name="loading">Loading...</string>
    <string name="no_data">No Data</string>
</resources>
```

---

## 🔄 核心业务流程

### 1. 证明材料提交流程

```
用户打开 ProofAndNFTActivity
         ↓
    点击"Select Proof File" → 选择多个文件
         ↓
    （可选）点击"Select Photo for NFT Minting" → 选择图片
         ↓
    输入昵称、选择是否显示代表作
         ↓
    点击"Submit Proof"
         ↓
    ProofUploadUtil.uploadBatch()
         ↓
    生成批次ID (timestamp + random)
         ↓
    上传证明文件到后端 /api/files/upload-batch
         ↓
    （如果有）上传NFT图片到后端 /api/files/upload-nft-image
         ↓
    显示成功消息（英文）
         ↓
    用户可在 MyCenterActivity 查看提交历史
```

### 2. 地址切换缓存清理流程

```
用户切换钱包账户
         ↓
    打开 MyCenterActivity
         ↓
    调用 checkAndRestoreNftCache()
         ↓
    获取当前钱包地址
         ↓
    比较 cachedWalletAddress 和当前地址
         ↓
    如果地址相同 → 恢复缓存
         ↓
    如果地址不同 → 调用 clearNftCache()
         ↓
    清空所有静态缓存变量
         ↓
    重新加载数据
         ↓
    保存新地址和新数据到缓存
```

### 3. NFT分页加载流程

```
打开 MyCenterActivity 或 GlobalStatsActivity
         ↓
    初始化 page = 0, size = 10
         ↓
    调用 loadMyNfts(0) 或 loadGlobalNfts(0)
         ↓
    请求 API: /api/blockchain/nfts/user/{address}?page=0&size=10
         ↓
    后端从区块链查询NFT（倒序）
         ↓
    返回 JSON: { nfts: [...], hasMore: true, total: 50 }
         ↓
    解析数据，添加到 nftList
         ↓
    更新 NFTViewAdapter
         ↓
    用户滑动到底部 → 触发加载更多
         ↓
    page++, 调用 loadMyNfts(1)
         ↓
    追加新数据到列表
         ↓
    直到 hasMore = false → 显示"End of list"
```

---

## 🛠️ 技术栈

### 核心框架
- **Android SDK** - 原生Android开发
- **Java** - 编程语言

### UI组件
- **RecyclerView** - 列表展示
- **CardView** - 卡片布局
- **SwipeRefreshLayout** - 下拉刷新
- **AlertDialog** - 对话框

### 网络请求
- **OkHttp** - HTTP客户端
- **JSON** - 数据解析

### 图片加载
- **Glide** - 图片加载和缓存

### 其他
- **SharedPreferences** - 本地数据存储
- **Intent** - 页面跳转和文件选择

---

## 📝 配置说明

### 服务器地址配置

**位置：** `app/src/main/java/com/example/brokerfi/config/ServerConfig.java`

```java
public class ServerConfig {
    // 本地开发（USB调试）
    public static final String BASE_URL = "http://192.168.1.100:5000";
    
    // 云服务器部署
    // public static final String BASE_URL = "http://your-domain.com:5000";
}
```

**⚠️ 重要：** 部署到云服务器时需要修改此配置！

---

## 🚀 构建与运行

### 开发环境运行

```bash
# 1. 打开Android Studio
# 2. 导入项目：brokerwallet-academic
# 3. 连接手机（开启USB调试）
# 4. 点击运行按钮

# 或使用命令行
cd brokerwallet-academic
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

### USB调试端口转发

```bash
# 将手机的5000端口转发到电脑的5000端口
adb reverse tcp:5000 tcp:5000

# 验证
adb reverse --list
```

---

## 🆘 常见问题

### Q1: 网络请求失败
**解决：** 
1. 检查后端是否启动
2. 检查 `ServerConfig.BASE_URL` 是否正确
3. 确保执行了 `adb reverse tcp:5000 tcp:5000`

### Q2: 图片加载失败
**解决：** 
1. 检查图片URL是否正确
2. 检查网络权限
3. 查看Glide错误日志

### Q3: 地址切换后数据未刷新
**解决：** 
1. 检查 `checkAndRestoreNftCache()` 是否被调用
2. 查看日志确认缓存是否被清空
3. 手动下拉刷新

### Q4: 文件选择失败
**解决：** 
1. 检查存储权限
2. 使用"文件选择帮助"提示的方法
3. 尝试从相册或文件管理器选择

---

## 🌐 Gold 预测市场 API 文档（Agent 模块）

以下 API 由 `agent/gold/` 模块调用，用于前端与 Go 后端 PostgreSQL 数据库之间的数据同步。

**API 基础路径：** `{BASE_URL}/api/v1/gold`

**BASE_URL 自动切换规则：**
- Debug 构建：`http://10.0.2.2:8081`（Android 模拟器 → 宿主机 localhost）
- Release 构建：`https://dash.broker-chain.com:440`（生产服务器）

---

### 📖 读取 API（GET）

#### 1. 获取所有游戏元数据
```
GET /api/v1/gold/games
Response: { "games": [ GameMetaDTO, ... ] }
```
从后端 DB 批量获取所有游戏的标题、条件、图片等元数据。比从 IPFS 逐个下载快。

#### 2. 获取单个游戏元数据
```
GET /api/v1/gold/games/{gameId}
Response: GameMetaDTO
```

#### 3. 获取缓存的链上状态
```
GET /api/v1/gold/games/{gameId}/chain-state?user_address=0x...
Response: ChainStateDTO
```
从后端 DB 获取缓存的链上状态（避免直接 eth_call 的延迟）。

#### 4. 批量获取所有游戏的链上缓存状态
```
GET /api/v1/gold/games/chain-states?user_address=0x...
Response: { "states": [ ChainStateDTO, ... ] }
```

#### 5. 获取历史价格数据
```
GET /api/v1/gold/games/{gameId}/history
Response: { "history": [ HistoryPointDTO, ... ] }
```

#### 6. 查询 AI 托管状态
```
GET /api/v1/gold/ai-managed?game_id=X&user_address=Y&contract_address=Z
Response: { "enabled": true/false }
```

---

### ✍️ 写入 API（POST）— 链上交易后同步到后端 DB

以下 API 在**前端向链上写入数据后同步调用**，确保后端 DB 缓存与链上状态保持一致。
写入流程：**IPFS 上传 → 链上交易 → 后端 DB 同步（异步，失败不阻塞主流程）**

---

#### 1. 同步游戏元数据
```
POST /api/v1/gold/games/sync
```
**调用时机：** 创建博弈池交易确认后。

**完整同步流程（createGame）：**
1. 通过 `gameCount()` eth_call 获取新博弈池的 gameId
2. 调用 `/games/sync` 同步元数据（标题、条件、IPFS CID 等）
3. 调用 `/games/{gameId}/chain-state/sync` 同步初始链上状态（资金池、储备金）
4. 调用 `/games/{gameId}/history` 添加初始历史价格点（YES=50%, NO=50%）

**Request Body (GameMetaSyncReq):**
```json
{
  "game_id": 0,
  "contract_address": "0x...",
  "ipfs_cid": "Qm...",
  "desc": "博弈池描述",
  "condition": "判定条件",
  "avatar_url": "Qm... (IPFS CID of avatar image)",
  "detailed_info": "详细信息",
  "option_yes": "YES 选项名",
  "option_no": "NO 选项名",
  "creator_address": "0x...",
  "duration_sec": 86400,
  "initial_liquidity_wei": "1000000000000000000"
}
```
**Response:**
```json
{ "success": true, "game_id": 123 }
```

**对应 Java 方法：** `BackendApiClient.syncGameMetadata(GameMetaSyncReq req)`

---

#### 2. 同步链上状态缓存
```
POST /api/v1/gold/games/{gameId}/chain-state/sync
```
**调用时机：** 每次链上交易确认后（buyShares / sellShares / claimReward / resolveGame / createGame），通过 eth_call 查询交易后的真实链上状态，同步写入后端 DB 缓存。确保后续读取操作优先命中 DB 缓存，避免直接 eth_call 的延迟。

**数据来源：** 交易确认后通过 `getGameInfo()` + `getGameExtraData()` eth_call 查询链上真实值，非空占位符。

**Request Body (ChainStateSyncReq):**
```json
{
  "total_pool": "5000000000000000000",
  "is_resolved": false,
  "is_refunded": false,
  "winning_option": 0,
  "reserve_yes": "3000000000000000000",
  "reserve_no": "2000000000000000000",
  "my_shares_yes": "1500000000000000000",
  "my_shares_no": "0"
}
```
**Response:**
```json
{ "success": true }
```

**对应 Java 方法：** `BackendApiClient.syncChainState(int gameId, ChainStateSyncReq req)`

**覆盖的写操作：**
| 操作 | 触发方法 | tradeType | 备注 |
|------|----------|-----------|------|
| 买入份额 | `buyShares()` | BUY | 同步资金池、储备金、用户持仓变化（eth_call 真实值） |
| 卖出份额 | `sellShares()` | SELL | 同上 |
| 领取奖励 | `claimReward()` | CLAIM | 同步用户持仓归零 |
| 结算博弈池 | `resolveGame()` | RESOLVE | 同步 isResolved=true 和 winningOption |
| 创建博弈池 | `createGame()` | — | 同步初始资金池和储备金状态 |

---

#### 3. 添加历史价格点
```
POST /api/v1/gold/games/{gameId}/history
```
**调用时机：** 每次交易确认后，记录当前 YES/NO 价格快照，用于绘制折线图。

**Request Body (HistoryPointDTO):**
```json
{
  "game_id": 123,
  "timestamp_sec": 1719100000,
  "yes_price": 60.5,
  "no_price": 39.5,
  "total_pool": "5000000000000000000"
}
```
**Response:**
```json
{ "success": true }
```

**对应 Java 方法：** `BackendApiClient.addHistoryPoint(int gameId, HistoryPointDTO point)`

---

#### 4. 同步交易记录
```
POST /api/v1/gold/trades/sync
```
**调用时机：** 每次链上交易确认后，记录交易详情到后端 DB。

**Request Body (TradeSyncReq):**
```json
{
  "game_id": 123,
  "contract_address": "0x...",
  "user_address": "0x...",
  "trade_type": "BUY",
  "option_id": 0,
  "amount_wei": "1000000000000000000",
  "tx_hash": "0x...",
  "is_success": true,
  "total_pool_after": "5000000000000000000",
  "reserve_yes_after": "3000000000000000000",
  "reserve_no_after": "2000000000000000000",
  "my_shares_yes_after": "1500000000000000000",
  "my_shares_no_after": "0"
}
```

**trade_type 枚举值：**
| 值 | 含义 |
|----|------|
| `BUY` | 买入份额 |
| `SELL` | 卖出份额 |
| `CLAIM` | 领取奖励 |
| `RESOLVE` | 结算博弈池 |

**Response:**
```json
{ "success": true }
```

**对应 Java 方法：** `BackendApiClient.syncTrade(TradeSyncReq req)`

---

#### 5. 设置 AI 托管状态
```
POST /api/v1/gold/ai-managed
```
**调用时机：** 用户切换 AI 托管开关时。

**Request Body:**
```json
{
  "game_id": 123,
  "user_address": "0x...",
  "enabled": true,
  "contract_address": "0x...",
  "private_key": "0x..."
}
```
**Response:**
```json
{ "success": true }
```

**对应 Java 方法：** `BackendApiClient.setAiManagedStatus(...)`

---

### 🔄 写入数据流（完整时序）

```
用户操作 (UI)
  │
  ▼
ViewModel (GoldMarketDetailViewModel / GoldCreatePoolViewModel)
  │
  ▼
GoldMarketRepository
  │
  ├─ 1. IPFS 上传 (createGame 专属)
  │     ├─ PinataClient.uploadFileToIPFS() → 图片 CID
  │     └─ PinataClient.uploadJsonToIPFS() → 元数据 CID
  │
  ├─ 2. 发送链上交易
  │     ├─ LocalRPC 模式: sendLocalRpcAndWait() → web3j.ethSendTransaction()
  │     └─ BrokerChain 模式: sendBrokerChainTx() → BrokerChainClient.sendEthTx()
  │
  ├─ 3. 等待链上确认
  │     ├─ LocalRPC 模式: waitForLocalReceipt() 轮询 ethGetTransactionReceipt (最多30秒)
  │     └─ BrokerChain 模式: Thread.sleep(8000) 等待链上打包
  │
  ├─ 4. 查询交易后链上真实状态  ← ← ← 【新增】
  │     └─ queryPostTxState(gameId)
  │         ├─ eth_call getGameInfo → totalPool, isResolved, winningOption...
  │         └─ eth_call getGameExtraData → reserveYES, reserveNO, mySharesYES, mySharesNO
  │
  ├─ 5. 同步到后端 DB（在 onConfirmed 之前） ← ← ← 【重构】
  │     ├─ POST /api/v1/gold/trades/sync             (同步交易记录，含真实链上数据)
  │     ├─ POST /api/v1/gold/games/{id}/history       (添加历史价格点)
  │     ├─ POST /api/v1/gold/games/{id}/chain-state/sync (同步链上状态缓存)
  │     └─ POST /api/v1/gold/games/sync               (createGame 专属: 同步元数据)
  │
  └─ 6. 回调 onConfirmed 通知 UI ← ← ← 此时后端 DB 已包含最新数据
```

**设计原则（重要变更）：**
- ✅ **onConfirmed 回调时，后端 DB 已写入完成**——UI 无需额外等待，可立即从后端 DB 拉取最新状态
- ✅ **使用链上真实状态**——通过 eth_call 查询交易后的实际链上数据，而非传空值
- ✅ **每项同步独立 try-catch**——交易记录、历史价格、链上状态任一项失败不影响其他项
- ✅ **后端同步失败不阻塞主流程**——仅记录日志（`Log.w`），仍会正常回调 onConfirmed
- ✅ **后续读取优先命中后端 DB 缓存**——避免直接 eth_call 的延迟

---

### 🆕 新增：获取用户交易历史（v1.1 个人持仓详情页）

> **背景**：v1.1 新增了「个人持仓详情页」（`GoldPositionDetailActivity`），用户点击持仓卡片后可以查看每笔购买的详细记录、AI/手动标签和收益率。需要后端提供交易历史查询 API。

---

#### 6. 查询用户交易历史（新增）

```
GET /api/v1/gold/trades?game_id={gameId}&user_address={userAddress}
```

**调用时机：** 用户进入个人持仓详情页（`GoldPositionDetailActivity`）时。

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `game_id` | int | ✅ | 博弈池 ID |
| `user_address` | string | ✅ | 用户钱包地址（0x 开头） |

**Response:**
```json
{
  "trades": [
    {
      "trade_type": "BUY",
      "option_id": 0,
      "amount_wei": "1000000000000000000",
      "share_amount_wei": "12500000000000000000",
      "my_shares_yes_after": "12500000000000000000",
      "my_shares_no_after": "0",
      "is_success": true,
      "is_ai_managed": false,
      "tx_hash": "0xabc123...",
      "created_at": "2026-06-29 14:30:00"
    },
    {
      "trade_type": "BUY",
      "option_id": 1,
      "amount_wei": "500000000000000000",
      "share_amount_wei": "6200000000000000000",
      "my_shares_yes_after": "12500000000000000000",
      "my_shares_no_after": "6200000000000000000",
      "is_success": true,
      "is_ai_managed": true,
      "tx_hash": "0xdef456...",
      "created_at": "2026-06-28 09:15:00"
    }
  ]
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `trade_type` | string | 交易类型：`BUY` / `SELL` / `CLAIM`。前端仅展示 `BUY` 类型 |
| `option_id` | int | 0 = YES, 1 = NO |
| `amount_wei` | string | 用户支付的 BKC 金额（wei 单位，18 位小数） |
| `share_amount_wei` | string | 🆕 用户实际获得的份额数（wei 单位，18 位小数）。前端用于展示"XX 份额" |
| `my_shares_yes_after` | string | 🆕 该笔交易完成后，用户持有的 YES 总份额（wei） |
| `my_shares_no_after` | string | 🆕 该笔交易完成后，用户持有的 NO 总份额（wei） |
| `is_success` | bool | 交易是否成功 |
| `is_ai_managed` | bool | 🆕 该笔交易是否由 AI 托管自动执行 |
| `tx_hash` | string | 链上交易哈希 |
| `created_at` | string | 交易时间（格式 `yyyy-MM-dd HH:mm:ss` 或 RFC3339）。禁止返回空串、`1970-01-01 00:00:00`、零时间 |

**前端行为：**
- 仅展示 `trade_type == "BUY"` 的记录（不展示 SELL/CLAIM）
- `option_id == 0` → 绿色 YES 标签；`option_id == 1` → 红色 NO 标签
- `is_ai_managed == true` → 显示紫色「AI托管」标签；`false` → 显示灰色「手动」标签
- `share_amount_wei` 为空或为 0 时，前端会尝试用 `my_shares_yes_after` / `my_shares_no_after` 反推本次成交份额
- 按 `created_at` 倒序排列展示
- 后端不可用时静默降级，显示「暂无交易记录」

**对应 Java 方法：** `BackendApiClient.fetchTradeHistory(int gameId, String userAddress)`

---

### ⚠️ 修改：交易同步接口新增字段

> 现有 `POST /api/v1/gold/trades/sync` 需要新增两个字段来支持持仓详情页。

#### 4（修订）. 同步交易记录

**Request Body (TradeSyncReq) — 新增字段：**
```json
{
  "game_id": 123,
  "contract_address": "0x...",
  "user_address": "0x...",
  "trade_type": "BUY",
  "option_id": 0,
  "amount_wei": "1000000000000000000",
  "tx_hash": "0x...",
  "is_success": true,

  "_comment_new_fields": "↓↓↓ 以下为 v1.1 新增字段 ↓↓↓",

  "share_amount_wei": "12500000000000000000",
  "is_ai_managed": false,

  "_comment_existing_fields": "↓↓↓ 以下为现有字段（保持不变）↓↓↓",

  "total_pool_after": "5000000000000000000",
  "reserve_yes_after": "3000000000000000000",
  "reserve_no_after": "2000000000000000000",
  "my_shares_yes_after": "1500000000000000000",
  "my_shares_no_after": "0"
}
```

**新增字段说明：**

| 字段 | 类型 | 说明 | 数据来源 |
|------|------|------|---------|
| `share_amount_wei` | string | 🆕 用户实际获得的份额数（wei）。从 buyShares 交易的 event logs 或 eth_call 获取 | 链上交易 receipt / event |
| `is_ai_managed` | bool | 🆕 该笔交易是否由 AI 托管执行。buyShares 时传入，同步时回写 DB | 前端 `GoldMarketRepository.buyShares()` 传入 |

---

## 🔧 后端需要修改的内容

### 一、数据库变更（`gold_trades` 表）

```sql
-- 1. 新增字段：是否 AI 托管执行
ALTER TABLE gold_trades 
ADD COLUMN is_ai_managed BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. 新增字段：用户实际获得的份额数（wei）
ALTER TABLE gold_trades 
ADD COLUMN share_amount_wei VARCHAR(78) NOT NULL DEFAULT '0';

-- 3. 为查询接口创建索引
CREATE INDEX idx_gold_trades_game_user 
ON gold_trades(game_id, user_address);
```

**字段说明：**

| 列名 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `is_ai_managed` | `BOOLEAN` | `FALSE` | 该笔交易是否由 AI 托管自动执行。buyShares 时根据 AI 托管开关状态写入 |
| `share_amount_wei` | `VARCHAR(78)` | `'0'` | 用户买入时实际获得的份额数（wei 单位）。从 buyShares 交易回执的 Transfer event 或 post-trade eth_call 的 myShares 差值计算 |

### 二、新增 API 接口

#### `GET /api/v1/gold/trades`

**路由注册（Go 示例）：**
```go
// router.go
goldGroup := router.Group("/api/v1/gold")
goldGroup.GET("/trades", handler.GetTradeHistory)
```

**Handler 实现要点：**
```go
// GET /api/v1/gold/trades?game_id={gameId}&user_address={userAddress}
func GetTradeHistory(c *gin.Context) {
    gameID, _ := strconv.Atoi(c.Query("game_id"))
    userAddress := c.Query("user_address")

    if gameID <= 0 || userAddress == "" {
        c.JSON(400, gin.H{"error": "game_id and user_address are required"})
        return
    }

    // 从 gold_trades 表查询
    rows, err := db.Query(`
        SELECT trade_type, option_id, amount_wei, share_amount_wei,
               is_success, is_ai_managed, tx_hash, created_at,
               my_shares_yes_after, my_shares_no_after
        FROM gold_trades
        WHERE game_id = $1 AND user_address = $2
        ORDER BY created_at DESC
    `, gameID, userAddress)

    // ... 扫描结果，返回 JSON
    c.JSON(200, gin.H{"trades": trades})
}
```

**返回格式：**
```json
{
  "trades": [
    {
      "trade_type": "BUY",
      "option_id": 0,
      "amount_wei": "1000000000000000000",
      "share_amount_wei": "12500000000000000000",
      "my_shares_yes_after": "12500000000000000000",
      "my_shares_no_after": "0",
      "is_success": true,
      "is_ai_managed": false,
      "tx_hash": "0x...",
      "created_at": "2026-06-29T14:30:00Z"
    }
  ]
}
```

### 三、修改现有 `POST /api/v1/gold/trades/sync` 接口

在现有 `syncTrade` handler 中，需要额外处理两个新字段：

```go
type TradeSyncReq struct {
    // ... 现有字段保持不变 ...

    // 🆕 v1.1 新增
    ShareAmountWei string `json:"share_amount_wei"` // 用户获得的份额数(wei)
    IsAiManaged    bool   `json:"is_ai_managed"`     // 是否 AI 托管
}
```

**2026-06-29 补充说明：**

- Android 客户端现已在交易确认后，用“交易前持仓”和“交易后持仓”的差值自动计算 `share_amount_wei`，并随 `POST /api/v1/gold/trades/sync` 一起上传。
- 后端 `GET /api/v1/gold/trades` 请务必原样返回 `share_amount_wei`，不要在查询层把空值替换成 `0`，除非数据库里确实就是 0。
- `created_at` 请返回真实入库时间；如果历史脏数据里出现 `1970-01-01 00:00:00`，需要做一次数据修复或在接口层过滤成真实创建时间。

**插入 SQL 更新（Go 示例）：**
```go
_, err := db.Exec(`
    INSERT INTO gold_trades (
        game_id, contract_address, user_address, trade_type,
        option_id, amount_wei, share_amount_wei, tx_hash,
        is_success, is_ai_managed,
        total_pool_after, reserve_yes_after, reserve_no_after,
        my_shares_yes_after, my_shares_no_after
    ) VALUES (
        $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
        $11, $12, $13, $14, $15
    )
`,
    req.GameID, req.ContractAddress, req.UserAddress, req.TradeType,
    req.OptionID, req.AmountWei, req.ShareAmountWei, req.TxHash,
    req.IsSuccess, req.IsAiManaged,
    req.TotalPoolAfter, req.ReserveYESAfter, req.ReserveNOAfter,
    req.MySharesYESAfter, req.MySharesNOAfter,
)
```

### 四、前端对应变更汇总

| 前端文件 | 变更 |
|----------|------|
| `BackendApiClient.java` | 新增 `fetchTradeHistory()` 方法 + `TradeDTO` 类（含 `share_amount_wei`, `is_ai_managed` 字段） |
| `GoldPositionDetailActivity.java` | 新增整个页面：池信息 + 持仓估值 + 交易记录列表 + 收益率 |
| `activity_gold_position_detail.xml` | 新布局：池信息卡片、持仓汇总、交易记录列表 |
| `item_trade_history.xml` | 单条交易行：色条 + YES/NO 标签 + AI/手动标签 + 时间 + 金额 + 份额 |
| `GoldMyPositionsFragment.java` | 点击持仓卡片跳转到 `GoldPositionDetailActivity`（原跳转到 `GoldMarketDetailActivity`） |
| `GoldMarketDetailViewModel.java` | 新增 `getWalletAddress()` 方法 |
| `AndroidManifest.xml` | 注册 `GoldPositionDetailActivity` |

### 五、兼容性说明

| 场景 | 前端行为 |
|------|---------|
| 后端尚未部署新接口 | `fetchTradeHistory()` 静默失败，显示「暂无交易记录」，池信息/持仓估值仍正常显示 |
| `share_amount_wei` 为 `"0"` 或空，但返回了 `my_shares_*_after` | 前端自动用持仓快照差值反推份额 |
| `share_amount_wei` 为 `"0"` 或空，且没有 `my_shares_*_after` | 显示「份额待同步」 |
| `is_ai_managed` 为 `false`（默认值） | 显示灰色「手动」标签 |
| 仅有 SELL/CLAIM 记录 | 过滤后为空，显示「暂无交易记录」 |
| 旧数据无 `is_ai_managed` 字段 | DB 默认值 `FALSE` → 显示「手动」 |
| `created_at` 返回空值或 Unix 零时间 | 前端显示「时间待同步」 |

---

## 📚 相关文档

- **项目总览：** `../../PROJECT_STRUCTURE.md`
- **后端文档：** `../../BrokerWallet-backend/PROJECT_STRUCTURE.md`
- **前端文档：** `../../brokerwallet-frontend/PROJECT_STRUCTURE.md`
- **部署指南：** `../../DEPLOYMENT_GUIDE.md`

---

**最后更新：** 2026年6月29日（v1.1 新增交易历史 API + 个人持仓详情页）
