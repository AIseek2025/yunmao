package live.yunmao.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController

// 简化版 UI 骨架；每个 screen 都用 ViewModel 拉数据（真实实现见各 vm/*.kt，第八轮 PoC 留 TODO）。

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LoginScreen(nav: NavHostController) {
    var phone by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("yunmao 云养猫", style = MaterialTheme.typography.headlineMedium)
        OutlinedTextField(value = phone, onValueChange = { phone = it }, label = { Text("手机号") })
        OutlinedTextField(value = code, onValueChange = { code = it }, label = { Text("验证码") })
        Button(onClick = { nav.navigate("rooms") }, enabled = phone.isNotEmpty() && code.isNotEmpty()) {
            Text("登录")
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RoomListScreen(nav: NavHostController) {
    val rooms = remember { listOf("room_demo" to "yunmao 示例房间") }
    Scaffold(topBar = { TopAppBar(title = { Text("直播房间") }) }) { padding ->
        LazyColumn(Modifier.padding(padding)) {
            items(rooms) { (id, name) ->
                ListItem(
                    headlineContent = { Text(name) },
                    supportingContent = { Text(id) },
                    trailingContent = { TextButton(onClick = { nav.navigate("room/$id") }) { Text("进入") } }
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RoomDetailScreen(roomId: String, navController: NavHostController) {
    var feedStatus by remember { mutableStateOf("idle") }
    Scaffold(topBar = { TopAppBar(title = { Text("房间 $roomId") }) }) { padding ->
        Column(Modifier.padding(padding).padding(16.dp)) {
            Surface(modifier = Modifier.fillMaxWidth().height(220.dp), tonalElevation = 4.dp) {
                Text("LL-HLS / WHEP 播放占位")
            }
            Spacer(Modifier.height(16.dp))
            Button(onClick = { feedStatus = "dispensing" }) { Text("投喂 5g") }
            Text("状态：$feedStatus")
            Spacer(Modifier.height(8.dp))
            Text("弹幕区（占位）")
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProfileScreen(nav: NavHostController) {
    Scaffold(topBar = { TopAppBar(title = { Text("我的") }) }) { padding ->
        Column(Modifier.padding(padding).padding(16.dp)) {
            Text("钱包余额：0 元", style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(8.dp))
            Button(onClick = { /* 调起 PayManager.startWeChat / startAlipay */ }) {
                Text("微信充值 30 元")
            }
        }
    }
}
