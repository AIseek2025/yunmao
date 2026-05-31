package live.yunmao.app.ui

import androidx.compose.runtime.Composable
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable

@Composable
fun NavGraph(navController: NavHostController) {
    NavHost(navController, startDestination = "login") {
        composable("login") { LoginScreen(navController) }
        composable("rooms") { RoomListScreen(navController) }
        composable("room/{id}") { backStack ->
            val id = backStack.arguments?.getString("id") ?: ""
            RoomDetailScreen(roomId = id, navController = navController)
        }
        composable("me") { ProfileScreen(navController) }
    }
}
